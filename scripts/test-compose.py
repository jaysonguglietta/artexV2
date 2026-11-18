#!/usr/bin/env python3
"""Test fresh Compose setup on a dedicated disposable Docker daemon.

Requires existing artex-local:hardened, artex-local:worker, artex-local:broker and artex-local:postgres
images. Uses synthetic credentials, an ephemeral loopback port and a unique
Compose project. A pre-existing worker bridge is never removed.
"""

import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


SOURCE = Path(__file__).resolve().parents[1]
NETWORK = "artex-isolated-workers"


class Fixture:
    def __init__(self, directory):
        self.directory = directory
        self.nonce = secrets.token_hex(8)
        self.project = "artex-compose-test-" + self.nonce
        self.network_label = "artex.test.compose.owner=" + self.nonce
        self.password = secrets.token_hex(24)
        self.database_password = secrets.token_hex(24)
        self.token = ""
        self.network_existed = None
        self.preexisting_network_id = None
        self.compose_attempted = False
        self.checks = []
        self.started = time.monotonic()
        self.command = ["docker", "compose", "--project-name", self.project,
                        "--env-file", ".env", "--env-file", ".env.workers",
                        "-f", "docker-compose.yml", "-f", "compose.workers.yml",
                        "-f", "smoke.override.yml"]

    def redact(self, text):
        for value in [self.password, self.database_password, self.token]:
            if value:
                text = text.replace(value, "[REDACTED]")
        return text

    def run(self, args, **kwargs):
        kwargs.setdefault("timeout", 60)
        kwargs.setdefault("cwd", self.directory)
        result = subprocess.run(args, capture_output=True, text=True, **kwargs)
        if result.returncode:
            raise RuntimeError(self.redact("Command failed: " + args[0] + "\n" + result.stderr))
        return result

    def check(self, name, condition):
        self.checks.append({"check": name, "passed": bool(condition)})
        if not condition:
            raise RuntimeError(name)
        print("PASS " + name, flush=True)

    def inspect_network(self):
        result = subprocess.run(["docker", "network", "inspect", NETWORK],
                                capture_output=True, text=True, timeout=20)
        if result.returncode:
            if "no such network" in result.stderr.lower() or "not found" in result.stderr.lower():
                return None
            raise RuntimeError(self.redact("Cannot inspect worker bridge: " + result.stderr))
        networks = json.loads(result.stdout)
        if len(networks) != 1 or networks[0].get("Name") != NETWORK:
            raise RuntimeError("Ambiguous worker bridge identity")
        return networks[0]

    def setup_and_run(self):
        for image in ["artex-local:hardened", "artex-local:worker", "artex-local:broker", "artex-local:postgres"]:
            self.run(["docker", "image", "inspect", image])
        existing = self.inspect_network()
        self.network_existed = existing is not None
        self.preexisting_network_id = existing.get("Id") if existing else None
        real_docker = shutil.which("docker")
        if not real_docker:
            raise RuntimeError("Docker CLI is required")
        for relative in ["docker-compose.yml", "compose.workers.yml", "scripts/setup-workers.sh",
                         "worker/policy.example.json"]:
            target = self.directory / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(SOURCE / relative, target)
        compose = self.directory / "docker-compose.yml"
        compose.write_text(compose.read_text().replace("127.0.0.1:8787:8787", "127.0.0.1::8787"))
        environment = self.directory / ".env"
        environment.write_text("POSTGRES_PASSWORD=" + self.database_password +
                               "\nARTEX_ADMIN_PASSWORD=" + self.password + "\n")
        environment.chmod(0o600)
        (self.directory / "smoke.override.yml").write_text("services:\n  artex:\n    image: artex-local:hardened\n")

        # Tag only a network actually created by this setup invocation. Checking
        # absence alone is insufficient: another setup could create it meanwhile.
        binary_directory = self.directory / "bin"
        binary_directory.mkdir()
        wrapper = binary_directory / "docker"
        wrapper.write_text("#!" + sys.executable + "\nimport os,sys\nargs=sys.argv[1:]\n" +
                           "if args[:2]==['network','create'] and args[-1]=='artex-isolated-workers':\n" +
                           " args[-1:-1]=['--label'," + repr(self.network_label) + "]\n" +
                           "os.execv(" + repr(real_docker) + ",[" + repr(real_docker) + ",*args])\n")
        wrapper.chmod(0o700)
        setup_environment = dict(os.environ)
        setup_environment["PATH"] = str(binary_directory) + os.pathsep + os.environ.get("PATH", "")
        self.run(["/bin/sh", "scripts/setup-workers.sh"], env=setup_environment)
        self.check("Fresh setup creates or verifies the dedicated worker bridge", True)
        token = (self.directory / ".secrets/worker-token").read_bytes()
        self.token = token.decode().strip()
        self.run(["/bin/sh", "scripts/setup-workers.sh"], env=setup_environment)
        self.check("Repeated setup preserves the private broker token",
                   token == (self.directory / ".secrets/worker-token").read_bytes())
        policy_file = self.directory / ".secrets/worker-policy.json"
        policy = json.loads(policy_file.read_text())
        policy["sessions"] = {"task-99": ["198.18.0.10/32"]}
        policy_file.write_text(json.dumps(policy))
        self.run(self.command + ["config", "--quiet"])
        self.check("Combined production Compose configuration validates", True)
        self.compose_attempted = True
        self.run(self.command + ["up", "-d", "--no-build", "--wait", "--wait-timeout", "90"], timeout=150)
        port = self.run(self.command + ["port", "artex", "8787"]).stdout.strip()
        if not re.fullmatch(r"127\.0\.0\.1:[0-9]+", port):
            raise RuntimeError("Expected an ephemeral IPv4 loopback API port")
        base = "http://" + port
        for _ in range(60):
            try:
                with urllib.request.urlopen(base + "/api/auth/status", timeout=2) as response:
                    if response.status == 200:
                        break
            except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.RemoteDisconnected):
                time.sleep(1)
        else:
            raise RuntimeError("Compose API did not become healthy")
        request = urllib.request.Request(base + "/api/auth/login",
            data=json.dumps({"username": "ARTEX", "password": self.password}).encode(),
            headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(request, timeout=20) as response:
            self.check("Fresh Compose API and PostgreSQL initialize and authenticate", response.status == 200)
        database = self.run(self.command + ["ps", "-q", "postgres"]).stdout.strip()
        expected_uid = self.run(["docker", "exec", database, "id", "-u", "postgres"]).stdout.strip()
        process_status = self.run(["docker", "exec", database, "cat", "/proc/1/status"]).stdout
        uid_fields = next(line.split()[1:] for line in process_status.splitlines() if line.startswith("Uid:"))
        self.check("PostgreSQL native startup helper drops all server UIDs away from root",
                   expected_uid != "0" and uid_fields == [expected_uid] * 4)
        self.run(["docker", "exec", database, "psql", "-U", "artex", "-d", "artex", "-v", "ON_ERROR_STOP=1",
                  "-c", "CREATE TABLE security_restart_probe(value integer); INSERT INTO security_restart_probe VALUES (42);"])
        self.run(["docker", "restart", "--time", "20", database])
        for _ in range(60):
            ready = subprocess.run(["docker", "exec", database, "pg_isready", "-U", "artex"],
                                   capture_output=True, text=True, timeout=5)
            if ready.returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError("PostgreSQL did not recover after restart")
        persisted = self.run(["docker", "exec", database, "psql", "-U", "artex", "-d", "artex",
                              "-At", "-c", "SELECT value FROM security_restart_probe"]).stdout.strip()
        self.check("PostgreSQL native helper preserves initialized data across restart", persisted == "42")
        app = self.run(self.command + ["ps", "-q", "artex"]).stdout.strip()
        missing_interpreters = []
        for executable in ["/bin/sh", "/usr/bin/python3", "/usr/local/bin/node", "/usr/bin/curl"]:
            result = subprocess.run(["docker", "exec", app, executable, "--version"],
                                    capture_output=True, text=True, timeout=10)
            diagnostic = (result.stdout + result.stderr).lower()
            missing_interpreters.append(result.returncode != 0 and "no such file or directory" in diagnostic)
        self.check("Static API image contains no shell, Python, Node or curl", all(missing_interpreters))
        probe = '''import json,urllib.request
from pathlib import Path
token=Path('/run/probe-token').read_text().strip()
identity={'session':'task-99','run_id':RUN_ID}
def call(path,payload):
 req=urllib.request.Request('http://worker-broker:8790'+path,data=json.dumps(payload).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
 with urllib.request.urlopen(req,timeout=40) as response:return json.load(response)
try:
 result=call('/v1/call',{**identity,'tool':'Bash','input':{'command':'printf compose-worker-ok'}})
 assert not result.get('error'), 'worker tool call failed'
 assert 'compose-worker-ok' in json.dumps(result), 'worker result missing'
finally:
 call('/v1/close',identity)
print('worker probe passed')
'''.replace("RUN_ID", repr(secrets.token_hex(16)))
        # The API is a static scratch image. A disposable, unprivileged test
        # client shares only its network namespace to exercise the real route.
        self.run(["docker", "run", "--rm", "--name", self.project + "-network-probe",
                  "--label", "com.docker.compose.project=" + self.project,
                  "--network", "container:" + app, "--read-only", "--cap-drop", "ALL",
                  "--security-opt", "no-new-privileges:true", "--user", "10001:10001",
                  "--memory", "128m", "--cpus", "1", "--pids-limit", "32",
                  "--mount", "type=bind,src=" + str(self.directory / ".secrets/worker-token") + ",dst=/run/probe-token,readonly",
                  "--entrypoint", "python3", "artex-local:worker", "-c", probe])
        self.check("Compose API reaches broker and executes an isolated tool with cleanup", True)
        api_networks = json.loads(self.run(["docker", "inspect", app, "--format",
                                            "{{json .NetworkSettings.Networks}}"]).stdout)
        self.check("API is never attached to the worker bridge", NETWORK not in api_networks)

    def cleanup(self):
        errors = []
        if self.compose_attempted:
            try:
                self.run(self.command + ["down", "--volumes", "--remove-orphans", "--timeout", "20"], timeout=90)
            except Exception as error:
                errors.append(self.redact(str(error)))
        # A failed close or interrupted Compose startup must not leave this
        # fixture's workers or partially created project resources behind.
        token_file = self.directory / ".secrets/worker-token"
        try:
            if token_file.exists():
                self.token = token_file.read_text().strip()
        except Exception as error:
            errors.append(self.redact(str(error)))
        resources = []
        if self.token:
            owner = hashlib.sha256(self.token.encode()).hexdigest()[:16]
            resources.append((["ps", "-aq"], "artex.worker.owner=" + owner, ["rm", "-f"]))
        project_label = "com.docker.compose.project=" + self.project
        resources.extend([(["ps", "-aq"], project_label, ["rm", "-f"]),
                          (["network", "ls", "-q"], project_label, ["network", "rm"]),
                          (["volume", "ls", "-q"], project_label, ["volume", "rm"])])
        for listing, label, removal in resources:
            try:
                ids = self.run(["docker", *listing, "--filter", "label=" + label]).stdout.split()
                if ids:
                    self.run(["docker", *removal, *ids])
                if self.run(["docker", *listing, "--filter", "label=" + label]).stdout.split():
                    raise RuntimeError("Fixture resources remain after cleanup")
            except Exception as error:
                errors.append(self.redact(str(error)))
        project_cleanup_ok = not errors
        network_errors = []
        try:
            network = self.inspect_network()
            # Match the unique ownership label AND ID. Never remove a bridge
            # present before this fixture or one created by a concurrent setup.
            if self.network_existed is False and network and (network.get("Labels") or {}).get("artex.test.compose.owner") == self.nonce:
                self.run(["docker", "network", "rm", network["Id"]], timeout=20)
                remaining = self.inspect_network()
                if remaining and remaining.get("Id") == network["Id"]:
                    raise RuntimeError("Fixture-owned worker bridge remains after cleanup")
            if self.network_existed is True and (network is None or network.get("Id") != self.preexisting_network_id):
                raise RuntimeError("Pre-existing worker bridge identity changed during fixture execution")
        except Exception as error:
            network_errors.append(self.redact(str(error)))
        errors.extend(network_errors)
        try:
            shutil.rmtree(self.directory)
        except Exception as error:
            errors.append(self.redact(str(error)))
            project_cleanup_ok = False
        self.checks.append({"check": "Disposable Compose containers, volumes, service networks and fixture files removed",
                            "passed": project_cleanup_ok})
        self.checks.append({"check": "Only the worker bridge created by this fixture is removed; pre-existing bridges preserved",
                            "passed": not network_errors})
        return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, default=None)
    args = parser.parse_args()
    override = os.environ.get("ARTEX_COMPOSE_TEST_RESULTS")
    report = args.report or (Path(override) if override else Path(tempfile.mkdtemp(prefix="artex-compose-results-")) / "results.json")
    report = report.resolve()
    report.parent.mkdir(parents=True, exist_ok=True)
    fixture = Fixture(Path(tempfile.mkdtemp(prefix=".worker-fixture.compose-", dir=SOURCE)))
    failure = None
    completed = False
    try:
        fixture.setup_and_run()
        completed = True
    except KeyboardInterrupt:
        failure = "Interrupted before all Compose checks completed"
    except Exception as error:
        failure = fixture.redact(str(error))
    finally:
        cleanup_errors = fixture.cleanup()
        summary = {"schema_version": 1,
                   "passed": completed and failure is None and not cleanup_errors and all(item["passed"] for item in fixture.checks),
                   "elapsed_seconds": round(time.monotonic() - fixture.started, 2),
                   "checks": fixture.checks}
        if failure:
            summary["failure"] = failure
        if cleanup_errors:
            summary["cleanup_errors"] = cleanup_errors
        with report.open("w") as output:
            os.chmod(report, 0o600)
            json.dump(summary, output, indent=2)
            output.write("\n")
    passed = sum(item["passed"] for item in summary["checks"])
    print(f'{passed}/{len(summary["checks"])} Compose checks passed; report: {report}', flush=True)
    if not summary["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()

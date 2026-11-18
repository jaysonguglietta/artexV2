#!/usr/bin/env python3
"""Check worker setup with a fake Docker executable; no daemon is contacted."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parents[1]
IMAGE_ID = "sha256:" + "a" * 64
OWNED_NETWORK = {
    "Name": "artex-isolated-workers",
    "Driver": "bridge",
    "Scope": "local",
    "Internal": False,
    "Labels": {"artex.purpose": "isolated-worker-network"},
}
FAKE_DOCKER = r'''
import json, os, pathlib, sys
args = sys.argv[1:]
root = pathlib.Path(os.environ["FAKE_DOCKER_STATE"])
with (root / "calls.jsonl").open("a") as log:
    log.write(json.dumps(args) + "\n")
if args[:2] == ["image", "inspect"]:
    if args[2] == "artex-local:worker":
        print("sha256:" + "a" * 64)
elif args[0] == "run":
    print("987")
elif args[:2] == ["network", "inspect"]:
    state = root / "network.json"
    if not state.exists():
        sys.exit(1)
    print(state.read_text())
elif args[:2] == ["network", "create"]:
    if os.environ.get("FAKE_DOCKER_CREATE_FAIL") == "1":
        sys.exit(1)
    network = {"Name": args[-1], "Driver": "bridge", "Scope": "local",
               "Internal": False,
               "Labels": {"artex.purpose": "isolated-worker-network"}}
    (root / "network.json").write_text(json.dumps(network))
    print("synthetic-network-id")
else:
    sys.exit("unexpected Docker call: " + repr(args))
'''


class SetupWorkersTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="artex setup test ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for directory in ["scripts", "worker", "bin", "state"]:
            (self.root / directory).mkdir()
        shutil.copyfile(SOURCE / "scripts/setup-workers.sh", self.root / "scripts/setup-workers.sh")
        shutil.copyfile(SOURCE / "worker/policy.example.json", self.root / "worker/policy.example.json")
        docker = self.root / "bin/docker"
        docker.write_text("#!" + sys.executable + "\n" + FAKE_DOCKER)
        docker.chmod(0o755)
        self.environment = dict(os.environ)
        self.environment["PATH"] = str(self.root / "bin") + os.pathsep + os.environ["PATH"]
        self.environment["FAKE_DOCKER_STATE"] = str(self.root / "state")

    def invoke(self, *args):
        return subprocess.run(["/bin/sh", str(self.root / "scripts/setup-workers.sh"), *args],
                              env=self.environment, capture_output=True, text=True, check=False)

    def calls(self):
        log = self.root / "state/calls.jsonl"
        return [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []

    def set_network(self, network):
        (self.root / "state/network.json").write_text(json.dumps(network))

    def test_missing_network_is_created_labelled_and_inspected(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        creates = [call for call in self.calls() if call[:2] == ["network", "create"]]
        self.assertEqual(creates, [["network", "create", "--driver", "bridge", "--label",
                                    "artex.purpose=isolated-worker-network", "artex-isolated-workers"]])
        self.assertEqual(sum(call[:2] == ["network", "inspect"] for call in self.calls()), 2)
        self.assertEqual((self.root / ".env.workers").read_text(),
                         f"ARTEX_WORKER_IMAGE={IMAGE_ID}\nARTEX_DOCKER_GID=987\n")
        self.assertIn("compose down will not remove", result.stdout)

    def test_owned_network_is_reused_without_mutation_and_token_is_preserved(self):
        self.set_network(OWNED_NETWORK)
        (self.root / ".secrets").mkdir()
        token = "existing-private-token-" + "b" * 64 + "\n"
        (self.root / ".secrets/worker-token").write_text(token)
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(call[:2] == ["network", "create"] for call in self.calls()))
        self.assertEqual((self.root / ".secrets/worker-token").read_text(), token)

    def test_conflicting_network_is_rejected_without_adoption(self):
        for field, value in [("Driver", "overlay"), ("Scope", "swarm"), ("Internal", True),
                             ("Labels", {}), ("Labels", {"artex.purpose": "management"})]:
            with self.subTest(field=field, value=value):
                network = dict(OWNED_NETWORK)
                network[field] = value
                self.set_network(network)
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Refusing artex-isolated-workers", result.stderr)
                self.assertFalse((self.root / ".env.workers").exists())
                self.assertFalse(any(call[:2] == ["network", "create"] for call in self.calls()))

    def test_network_create_failure_does_not_publish_environment(self):
        self.environment["FAKE_DOCKER_CREATE_FAIL"] = "1"
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Cannot create the dedicated worker bridge", result.stderr)
        self.assertFalse((self.root / ".env.workers").exists())

    def test_secret_only_never_contacts_docker(self):
        result = self.invoke("--secret-only")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls(), [])
        self.assertFalse((self.root / ".env.workers").exists())
        self.assertRegex((self.root / ".secrets/worker-token").read_text(), r"^[a-f0-9]{64}\n$")
        self.assertEqual((self.root / ".secrets").stat().st_mode & 0o777, 0o700)
        self.assertEqual((self.root / ".secrets/worker-token").stat().st_mode & 0o777, 0o644)


if __name__ == "__main__":
    unittest.main(verbosity=2)

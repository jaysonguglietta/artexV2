#!/usr/bin/env python3
"""Exercise the real worker boundary using disposable synthetic Docker targets.

Build artex-local:worker and artex-local:broker first. This fixture needs a
DEDICATED Docker daemon: only its synthetic target receives host networking and
NET_ADMIN, to allocate two recorded benchmark addresses. No external address is
probed. All test containers, addresses, networks and state volumes are removed.
The orphan-watchdog case deliberately takes approximately 90 seconds.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request


class CheckFailure(Exception):
    pass


def docker(*args, timeout=35, check=True):
    result = subprocess.run(['docker', *args], capture_output=True, text=True,
                            timeout=timeout, check=False)
    if check and result.returncode:
        raise RuntimeError('Docker operation failed: ' + result.stderr.strip())
    return result.stdout.strip() if check else result


def text_result(payload):
    result = payload.get('result', {})
    content = result.get('Content', result.get('content', []))
    return '\n'.join(part.get('Text', part.get('text', '')) for part in content)


def unprivileged_status(status, uid):
    fields = dict(line.split(':', 1) for line in status.splitlines() if ':' in line)
    return (fields.get('Uid', '').split() == [str(uid)] * 4
            and fields.get('NoNewPrivs', '').strip() == '1'
            and all(fields.get(name, '').strip() == '0000000000000000'
                    for name in ['CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb']))


TARGET = r'''import http.server,ipaddress,json,pathlib,subprocess,sys
root=pathlib.Path('/fixture-state');assigned=[]
(root/'assigned.json').write_text('[]')
existing=json.loads(subprocess.check_output(['ip','-json','address','show'],text=True))
used={entry.get('local') for device in existing for entry in device.get('addr_info',[])}
for address in json.loads(sys.argv[1]):
 assert ipaddress.ip_address(address) in ipaddress.ip_network('198.18.0.0/15')
 if address in used:raise RuntimeError('synthetic address already in use')
 subprocess.run(['ip','address','add',address+'/32','dev','lo'],check=True)
 assigned.append(address);(root/'assigned.json').write_text(json.dumps(assigned))
counts={}
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path.startswith('/tick/'):
   label=self.path.split('/')[2];counts[label]=counts.get(label,0)+1
   temporary=root/'counts.tmp';temporary.write_text(json.dumps(counts));temporary.replace(root/'counts.json')
  self.send_response(200);self.end_headers();self.wfile.write(b'authorized synthetic target')
 def log_message(self,*args):pass
server=http.server.HTTPServer(('0.0.0.0',0),Handler)
(root/'ready.json').write_text(json.dumps({'port':server.server_port}))
server.serve_forever()
'''


class Fixture:
    def __init__(self, args, base):
        self.args, self.base = args, base
        self.token = secrets.token_hex(32)
        self.suffix = secrets.token_hex(4)
        self.network = f'artex-worker-test-{self.suffix}-net'
        self.target = f'artex-worker-test-{self.suffix}-target'
        self.broker = f'artex-worker-test-{self.suffix}-broker'
        self.volume = f'artex-worker-test-{self.suffix}-state'
        self.owner = hashlib.sha256(self.token.encode()).hexdigest()[:16]
        self.created_containers = []
        self.network_created = self.volume_created = False
        self.results = []
        self.active = set()
        self.active_lock = threading.Lock()
        self.heartbeat_stop = threading.Event()
        self.heartbeat_thread = None
        self.heartbeat_successes = 0
        self.heartbeat_errors = []
        self.endpoint = ''
        self.started = time.monotonic()
        self.target_state = base / 'target-state'
        self.target_state.mkdir()
        # Select adjacent synthetic addresses, rather than reusing another run's
        # host aliases or a fixed listening port. The target records only aliases
        # it successfully allocated, for precise finally cleanup.
        octet = 10 + secrets.randbelow(220)
        host = 2 + 2 * secrets.randbelow(100)
        self.allowed, self.denied = f'198.18.{octet}.{host}', f'198.18.{octet}.{host + 1}'
        self.worker_image = self.broker_image = ''

    def redact(self, value):
        return str(value).replace(self.token, '[REDACTED]')

    def check(self, name, okay, detail=None):
        result = {'check': name, 'passed': bool(okay)}
        if detail is not None:
            result['detail'] = self.redact(detail)
        self.results.append(result)
        print(('PASS ' if okay else 'FAIL ') + name, flush=True)
        if not okay:
            raise CheckFailure(name)

    def request(self, path, payload, auth=None, timeout=35):
        headers = {'Authorization': 'Bearer ' + (self.token if auth is None else auth),
                   'Content-Type': 'application/json'}
        req = urllib.request.Request(self.endpoint + path,
                                     data=json.dumps(payload).encode(), headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=timeout) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, {'error': self.redact(error.read().decode(errors='replace'))}

    def identity(self, label):
        return hashlib.sha256((self.suffix + '/' + label).encode()).hexdigest()[:32]

    def name(self, session, run_id):
        digest = hashlib.sha256(f'{session}/{run_id}/{self.token}'.encode()).hexdigest()[:20]
        return 'artex-worker-' + digest

    def call(self, session, run_id, command='', tool='Bash', heartbeat=True, auth=None,
             timeout=310):
        lifecycle = tool in ['__snapshot', '__resume', '__retire']
        body = {'session': session, 'tool': tool, 'input': {'command': command}}
        if not lifecycle:
            body['run_id'] = run_id
            if heartbeat and auth is None:
                with self.active_lock:
                    self.active.add((session, run_id))
        return self.request('/v1/call', body, auth=auth, timeout=timeout)

    def output(self, session, run_id, command, heartbeat=True):
        status, result = self.call(session, run_id, command, heartbeat=heartbeat)
        if status != 200 or result.get('error'):
            raise RuntimeError(f'worker call returned {status}: {self.redact(result.get("error", ""))}')
        if result.get('result', {}).get('IsError', result.get('result', {}).get('is_error', False)):
            raise RuntimeError('worker tool returned an execution error')
        return text_result(result)

    def close(self, session, run_id):
        with self.active_lock:
            self.active.discard((session, run_id))
        return self.request('/v1/close', {'session': session, 'run_id': run_id})

    def lease(self, session, run_id):
        return self.request('/v1/lease', {'session': session, 'run_id': run_id}, timeout=12)

    def retire(self, session):
        with self.active_lock:
            self.active = {key for key in self.active if key[0] != session}
        return self.call(session, None, tool='__retire')

    def start_heartbeat(self):
        self.heartbeat_stop.clear()
        def loop():
            while not self.heartbeat_stop.wait(20):
                with self.active_lock:
                    keys = list(self.active)
                for session, run_id in keys:
                    if self.heartbeat_stop.is_set():
                        return
                    try:
                        status, _ = self.lease(session, run_id)
                        if status != 200:
                            self.heartbeat_errors.append(f'lease status {status}')
                        else:
                            self.heartbeat_successes += 1
                    except (OSError, urllib.error.URLError) as error:
                        self.heartbeat_errors.append(type(error).__name__)
        self.heartbeat_thread = threading.Thread(target=loop, daemon=True)
        self.heartbeat_thread.start()

    def stop_heartbeat(self):
        self.heartbeat_stop.set()
        if self.heartbeat_thread:
            self.heartbeat_thread.join(timeout=14)
        self.heartbeat_thread = None

    def exists(self, name):
        return docker('inspect', name, timeout=10, check=False).returncode == 0

    def running(self, name):
        result = docker('inspect', name, '--format', '{{.State.Running}}', timeout=10, check=False)
        return result.returncode == 0 and result.stdout.strip() == 'true'

    def counts(self, label):
        path = self.target_state / 'counts.json'
        if not path.exists():
            return 0
        return json.loads(path.read_text()).get(label, 0)

    def wait_ticks(self, label, minimum=3):
        end = time.monotonic() + 10
        while time.monotonic() < end:
            if self.counts(label) >= minimum:
                return True
            time.sleep(0.15)
        return False

    def detached(self, session, run_id, label, heartbeat=True):
        # Double fork + setsid escapes the SDK's process groups and task manager.
        # Namespace/cgroup retirement must still stop these outbound requests.
        program = f'''import os,time,urllib.request
if os.fork():os._exit(0)
os.setsid()
if os.fork():os._exit(0)
opener=urllib.request.build_opener(urllib.request.ProxyHandler({{}}))
while True:
 try:opener.open('http://{self.allowed}:{self.port}/tick/{label}',timeout=1).read()
 except Exception:pass
 time.sleep(0.15)
'''
        self.output(session, run_id, 'python3 -c ' + shlex.quote(program) + ' </dev/null >/dev/null 2>&1',
                    heartbeat=heartbeat)
        self.check(f'{label}: detached double-fork positive control', self.wait_ticks(label))

    def refresh_endpoint(self):
        port = docker('port', self.broker, '8790/tcp').rsplit(':', 1)[1]
        self.endpoint = f'http://127.0.0.1:{port}'
        for _ in range(80):
            try:
                if self.request('/v1/call', {}, auth='wrong', timeout=2)[0] == 401:
                    return
            except (OSError, urllib.error.URLError):
                pass
            if not self.running(self.broker):
                raise RuntimeError('broker exited during startup: ' + self.redact(
                    docker('logs', '--tail', '10', self.broker, check=False).stderr))
            time.sleep(0.2)
        raise RuntimeError('broker startup deadline')

    def setup(self):
        self.worker_image = docker('image', 'inspect', self.args.worker_image, '--format', '{{.Id}}')
        self.broker_image = docker('image', 'inspect', self.args.broker_image, '--format', '{{.Id}}')
        (self.base / 'token').write_text(self.token)
        (self.base / 'token').chmod(0o644)
        policy = {'sessions': {'task-1': [self.allowed + '/32'], 'task-2': [self.allowed + '/32']},
                  'dns_servers': ['1.1.1.1'], 'management_cidrs': ['192.168.0.0/16']}
        (self.base / 'policy.json').write_text(json.dumps(policy))
        (self.base / 'policy.json').chmod(0o644)
        docker('network', 'create', self.network)
        self.network_created = True
        docker('volume', 'create', self.volume)
        self.volume_created = True
        docker('run', '--rm', '--user', '0:0', '--entrypoint', '/bin/sh',
               '-v', self.volume + ':/broker-state', self.broker_image, '-c',
               'chown 10001:10001 /broker-state\nchmod 0700 /broker-state')
        self.created_containers.append(self.target)
        docker('run', '-d', '--name', self.target, '--network', 'host', '--cap-add', 'NET_ADMIN',
               '--user', '0:0', '--entrypoint', 'python3',
               '-v', str(self.target_state) + ':/fixture-state', self.worker_image,
               '-c', TARGET, json.dumps([self.allowed, self.denied]))
        for _ in range(80):
            ready = self.target_state / 'ready.json'
            if ready.exists():
                self.port = json.loads(ready.read_text())['port']
                break
            if not self.running(self.target):
                raise RuntimeError('synthetic target failed to initialize')
            time.sleep(0.1)
        else:
            raise RuntimeError('synthetic target startup deadline')
        socket_gid = docker('run', '--rm', '--user', '0:0', '--entrypoint', 'stat',
                            '-v', '/var/run/docker.sock:/var/run/docker.sock', self.broker_image,
                            '-c', '%g', '/var/run/docker.sock')
        if not socket_gid.isdigit():
            raise RuntimeError('invalid daemon socket group')
        self.created_containers.append(self.broker)
        docker('run', '-d', '--name', self.broker, '--network', self.network,
               '--group-add', socket_gid, '--read-only', '--cap-drop', 'ALL',
               '--security-opt', 'no-new-privileges:true', '--memory', '512m', '--pids-limit', '64',
               '--cpus', '1', '--tmpfs', '/tmp:rw,nosuid,nodev,size=32m',
               '-p', '127.0.0.1::8790',
               '-v', '/var/run/docker.sock:/var/run/docker.sock',
               '-v', f'{self.base}/token:/run/secrets/worker-token:ro',
               '-v', f'{self.base}/policy.json:/run/worker-policy.json:ro',
               '-v', self.volume + ':/broker-state',
               '-e', 'ARTEX_WORKER_TOKEN_FILE=/run/secrets/worker-token',
               '-e', 'ARTEX_WORKER_POLICY_FILE=/run/worker-policy.json',
               '-e', 'ARTEX_WORKER_STATE_DIR=/broker-state',
               '-e', 'GOMEMLIMIT=384MiB', '-e', f'ARTEX_WORKER_IMAGE={self.worker_image}',
               '-e', f'ARTEX_WORKER_NETWORK={self.network}',
               '-e', 'ARTEX_PG_DSN=private-control-plane-canary', self.broker_image)
        self.refresh_endpoint()
        self.start_heartbeat()

    def run(self):
        a, b, c = [self.identity(name) for name in ['primary', 'sibling', 'other-task']]
        self.check('Unauthenticated broker access rejected',
                   self.call('task-1', a, 'true', auth='wrong')[0] == 401)
        self.check('Missing task target policy rejected',
                   self.call('task-999', a, 'true', heartbeat=False)[0] == 403)
        self.check('Normal RPC requires trusted run identity',
                   self.request('/v1/call', {'session': 'task-1', 'tool': 'Bash', 'input': {}})[0] == 400)
        self.check('Close control rejects executable request fields',
                   self.request('/v1/close', {'session': 'task-1', 'run_id': a, 'tool': 'Bash'})[0] == 400)
        absent = self.identity('never-created')
        self.check('Lease cannot create a worker', self.lease('task-1', absent)[0] == 200
                   and not self.exists(self.name('task-1', absent)))
        first = self.output('task-1', a, 'id; cat /proc/self/status; env; '
                            'test ! -S /var/run/docker.sock && echo NO_DOCKER; '
                            'test ! -e /run/secrets/worker-token && echo NO_BROKER_SECRET')
        self.check('Tool subprocess has UID10001, all capabilities empty and no-new-privileges',
                   unprivileged_status(first, 10001))
        self.check('API/broker credentials and service environment are absent',
                   'private-control-plane-canary' not in first and self.token not in first)
        self.check('Worker has no Docker socket or broker secret mount',
                   'NO_DOCKER' in first and 'NO_BROKER_SECRET' in first)
        allowed = f'http://{self.allowed}:{self.port}/'
        denied = f'http://{self.denied}:{self.port}/'
        self.check('Explicit synthetic target is reachable', 'authorized synthetic target' in
                   self.output('task-1', a, f'curl --max-time 3 --noproxy "*" {allowed}'))
        self.check('Denied-address positive control reaches the same target',
                   'authorized synthetic target' in docker('exec', self.broker, 'wget', '-q', '-T', '3', '-O', '-', denied))
        self.check('Adjacent unapproved synthetic address is denied', 'DENIED' in
                   self.output('task-1', a, f'curl --max-time 2 --noproxy "*" {denied} >/dev/null 2>&1 || echo DENIED'))
        gateway = docker('network', 'inspect', self.network, '--format', '{{(index .IPAM.Config 0).Gateway}}')
        self.check('Docker host gateway is denied', 'DENIED' in self.output('task-1', a,
                   f'curl --max-time 2 --noproxy "*" http://{gateway}:{self.port}/ >/dev/null 2>&1 || echo DENIED'))
        self.check('Worker cannot weaken its firewall', 'DENIED' in self.output(
            'task-1', a, 'iptables -P OUTPUT ACCEPT >/dev/null 2>&1 || echo DENIED'))
        self.check('Worker cannot renew the privileged watchdog', 'RENEW_DENIED' in self.output(
            'task-1', a, '/usr/local/bin/tool-worker renew >/dev/null 2>&1 && echo BAD || echo RENEW_DENIED'))
        signal_probe = 'import os,signal;\ntry:os.kill(1,signal.SIGUSR1);print("BAD")\nexcept PermissionError:print("SIGNAL_DENIED")'
        self.check('Worker cannot signal the root watchdog', 'SIGNAL_DENIED' in self.output(
            'task-1', a, 'python3 -c ' + shlex.quote(signal_probe)))
        self.output('task-1', a, 'printf primary-only > /workspace/private-primary')
        self.check('Same-task sibling has a different private filesystem', 'PRIVATE' in self.output(
            'task-1', b, 'test ! -e /workspace/private-primary && echo PRIVATE'))
        self.check('Different task filesystem is isolated', 'PRIVATE' in self.output(
            'task-2', c, 'test ! -e /workspace/private-primary && echo PRIVATE'))
        self.check('Same run retains its own workspace', 'primary-only' in self.output(
            'task-1', a, 'cat /workspace/private-primary'))
        self.check('Other-task cleanup succeeds', self.close('task-2', c)[0] == 200)
        self.detached('task-1', a, 'closed-run')
        self.detached('task-1', b, 'surviving-sibling')
        pool = concurrent.futures.ThreadPoolExecutor(max_workers=1)
        future = pool.submit(self.call, 'task-1', a, 'sleep 240')
        try:
            # Inspect the actual long-lived ForwardUnix client, rather than only
            # testing the daemon's Bash child or assuming docker exec inherited caps.
            probe = r'''import glob,json,pathlib
out=[]
for name in glob.glob('/proc/[0-9]*/cmdline'):
 try:
  raw=pathlib.Path(name).read_bytes()
  if raw==b'/usr/local/bin/tool-worker\x00call\x00':
   out.append(pathlib.Path(name).with_name('status').read_text())
 except (OSError,PermissionError):pass
print(json.dumps({'clients':out,'watchdog':pathlib.Path('/proc/1/status').read_text()}))
'''
            end = time.monotonic() + 12
            states = {}
            while time.monotonic() < end:
                raw = docker('exec', '--user', '0:0', '--workdir', '/', self.name('task-1', a),
                             '/usr/bin/setpriv', '--reuid=10001', '--regid=10001', '--clear-groups',
                             '--bounding-set=-all', '--inh-caps=-all', '--ambient-caps=-all',
                             '--no-new-privs', 'python3', '-c', probe)
                states = json.loads(raw)
                if states['clients']:
                    break
                time.sleep(0.2)
            self.check('Actual Docker exec RPC client has UID10001 and all capability sets empty',
                       bool(states.get('clients')) and all(unprivileged_status(s, 10001) for s in states['clients']))
            self.check('PID1 watchdog is root with all capability sets empty',
                       unprivileged_status(states.get('watchdog', ''), 0))
            self.check('Lease works while foreground scan holds execution gate', self.lease('task-1', a)[0] == 200)
            start = time.monotonic()
            self.check('Urgent close bypasses foreground execution gate', self.close('task-1', a)[0] == 200
                       and time.monotonic() - start < 12)
            status, _ = future.result(timeout=15)
            self.check('Urgent close cancels the active foreground request', status != 200)
        finally:
            # Ensure even a failed assertion cannot wait for the 240-second scan.
            self.close('task-1', a)
            pool.shutdown(wait=False, cancel_futures=True)
        self.check('Closed run cannot be replayed', self.call('task-1', a, 'true', heartbeat=False)[0] == 409)
        self.check('Lease cannot resurrect a closed run', self.lease('task-1', a)[0] == 200
                   and not self.exists(self.name('task-1', a)))
        time.sleep(0.4)
        stopped, surviving = self.counts('closed-run'), self.counts('surviving-sibling')
        time.sleep(1.2)
        self.check('Close stops unmanaged setsid/double-fork network descendants', self.counts('closed-run') == stopped)
        self.check('Closing one run preserves the sibling scan', self.counts('surviving-sibling') > surviving
                   and self.running(self.name('task-1', b)))

        orphan = self.identity('orphan-watchdog')
        self.detached('task-2', orphan, 'orphan', heartbeat=False)
        orphan_start = time.monotonic()
        print('Waiting for the independent 90-second orphan watchdog; sibling leases remain active.', flush=True)
        while self.running(self.name('task-2', orphan)) and time.monotonic() - orphan_start < 106:
            time.sleep(1)
        elapsed = time.monotonic() - orphan_start
        self.check('Unrenewed orphan container exits within the bounded watchdog lease',
                   not self.running(self.name('task-2', orphan)) and elapsed < 106,
                   f'observed {elapsed:.1f}s after initial RPC')
        time.sleep(0.4)
        stopped = self.counts('orphan')
        time.sleep(1)
        self.check('Watchdog exit stops orphan double-fork network descendants', self.counts('orphan') == stopped)
        self.check('Renewed idle sibling survives the orphan timeout', self.running(self.name('task-1', b))
                   and self.heartbeat_successes >= 3 and not self.heartbeat_errors)
        self.check('Expired orphan can be explicitly retired', self.close('task-2', orphan)[0] == 200)
        self.check('Surviving sibling cleanup succeeds', self.close('task-1', b)[0] == 200)

        s1, s2 = self.identity('snapshot-one'), self.identity('snapshot-two')
        self.output('task-1', s1, 'printf first-artifact > /workspace/first.txt')
        self.output('task-1', s2, 'printf second-artifact > /workspace/second.txt')
        status, snapshot = self.call('task-1', None, tool='__snapshot')
        files = snapshot.get('files', {})
        self.check('Real tmpfs snapshots preserve and merge all sibling artifacts', status == 200
                   and snapshot.get('snapshot_complete') and
                   base64.b64decode(files.get('first.txt', '')) == b'first-artifact' and
                   base64.b64decode(files.get('second.txt', '')) == b'second-artifact')
        self.check('All snapshot siblings are paused', all(docker('inspect', self.name('task-1', run_id),
                   '--format', '{{.State.Paused}}') == 'true' for run_id in [s1, s2]))
        self.check('Preservation failure can resume all paused siblings',
                   self.call('task-1', None, tool='__resume')[0] == 200 and
                   'first-artifact' in self.output('task-1', s1, 'cat /workspace/first.txt') and
                   'second-artifact' in self.output('task-1', s2, 'cat /workspace/second.txt'))
        self.check('Task retirement removes every sibling container', self.retire('task-1')[0] == 200
                   and all(not self.exists(self.name('task-1', run_id)) for run_id in [s1, s2]))

        conflict1, conflict2 = self.identity('conflict-one'), self.identity('conflict-two')
        self.output('task-1', conflict1, 'printf one > /workspace/conflict.txt')
        self.output('task-1', conflict2, 'printf two > /workspace/conflict.txt')
        status, _ = self.call('task-1', None, tool='__snapshot')
        self.check('Conflicting sibling artifacts explicitly refuse preservation', status == 409)
        self.check('Failed sibling snapshot resumes both runtimes',
                   'one' in self.output('task-1', conflict1, 'cat /workspace/conflict.txt') and
                   'two' in self.output('task-1', conflict2, 'cat /workspace/conflict.txt'))
        self.check('Conflicting task can be explicitly discarded', self.retire('task-1')[0] == 200)
        large = self.identity('oversized')
        self.output('task-1', large, 'truncate -s 5242880 /workspace/large.bin')
        self.check('Oversized tmpfs preservation fails explicitly',
                   self.call('task-1', None, tool='__snapshot')[0] == 409)
        self.check('Oversized task can be explicitly discarded', self.retire('task-1')[0] == 200)

        old = self.identity('crash-active')
        self.output('task-1', old, 'printf crash-artifact > /workspace/crash.txt')
        self.stop_heartbeat()
        with self.active_lock:
            self.active.clear()
        docker('kill', '--signal', 'KILL', self.broker)
        self.check('Crash fixture leaves an orphan for startup cleanup', self.running(self.name('task-1', old)))
        docker('start', self.broker)
        self.refresh_endpoint()
        self.check('Broker restart reaps its owner-labeled orphan', not self.exists(self.name('task-1', old)))
        self.check('Persistent ledger rejects a previously active identity after crash',
                   self.call('task-1', old, 'true', heartbeat=False)[0] == 409)
        self.check('Persistent ledger retains already-closed tombstones after crash',
                   self.call('task-1', a, 'true', heartbeat=False)[0] == 409)
        fresh = self.identity('fresh-after-restart')
        self.check('Fresh identity works without stale workspace after restart', 'FRESH' in self.output(
            'task-1', fresh, 'test ! -e /workspace/crash.txt && echo FRESH', heartbeat=False))
        self.check('Fresh replacement cleanup succeeds', self.close('task-1', fresh)[0] == 200)

    def cleanup(self):
        errors = []
        self.stop_heartbeat()
        # Even after broker crashes, remove only workers owned by this fixture's
        # token. The trusted Docker socket is never mounted into a worker.
        try:
            workers = docker('ps', '-aq', '--filter', 'label=artex.worker.owner=' + self.owner).split()
            if workers:
                docker('rm', '-f', *workers)
        except Exception as error:
            errors.append(type(error).__name__ + ': worker removal')
        assigned_file = self.target_state / 'assigned.json'
        try:
            assigned = json.loads(assigned_file.read_text()) if assigned_file.exists() else []
            approved = {self.allowed, self.denied}
            if any(address not in approved or ipaddress.ip_address(address) not in
                   ipaddress.ip_network('198.18.0.0/15') for address in assigned):
                raise RuntimeError('unexpected fixture address record')
            if assigned and self.worker_image:
                program = ('import json,subprocess,sys; '
                           '[subprocess.run(["ip","address","del",address+"/32","dev","lo"],check=True) '
                           'for address in json.loads(sys.argv[1])]')
                docker('run', '--rm', '--network', 'host', '--cap-add', 'NET_ADMIN', '--user', '0:0',
                       '--entrypoint', 'python3', self.worker_image, '-c', program, json.dumps(assigned))
        except Exception as error:
            errors.append(type(error).__name__ + ': synthetic address removal')
        for name in reversed(self.created_containers):
            try:
                if self.exists(name):
                    docker('rm', '-f', name)
            except Exception as error:
                errors.append(type(error).__name__ + ': fixture container removal')
        if self.network_created:
            try:
                docker('network', 'rm', self.network)
            except Exception as error:
                errors.append(type(error).__name__ + ': network removal')
        if self.volume_created:
            try:
                docker('volume', 'rm', self.volume)
            except Exception as error:
                errors.append(type(error).__name__ + ': private state volume removal')
        self.results.append({'check': 'All fixture containers, aliases, network and state volume removed',
                             'passed': not errors, 'detail': '; '.join(errors)})
        return errors


def main():
    root = Path(__file__).resolve().parents[1]
    override = os.environ.get('ARTEX_WORKER_TEST_RESULTS')
    default_report = Path(override) if override else Path(tempfile.mkdtemp(prefix='artex-worker-results-')) / 'results.json'
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-image', default='artex-local:worker')
    parser.add_argument('--broker-image', default='artex-local:broker')
    parser.add_argument('--report', type=Path, default=default_report)
    args = parser.parse_args()
    args.report.parent.mkdir(parents=True, exist_ok=True)
    failure = None
    completed = False
    with tempfile.TemporaryDirectory(prefix='.worker-fixture.', dir=root) as temporary:
        fixture = Fixture(args, Path(temporary))
        try:
            fixture.setup()
            fixture.run()
            completed = True
        except KeyboardInterrupt:
            failure = "Interrupted before all worker isolation checks completed"
            fixture.results.append({"check": "Fixture execution completed", "passed": False,
                                    "detail": failure})
        except Exception as error:
            failure = fixture.redact(str(error))
            if not isinstance(error, CheckFailure):
                fixture.results.append({'check': 'Fixture execution completed', 'passed': False,
                                        'detail': type(error).__name__ + ': ' + failure})
        finally:
            cleanup_errors = fixture.cleanup()
            summary = {'schema_version': 2, 'passed': completed and failure is None and not cleanup_errors,
                       'elapsed_seconds': round(time.monotonic() - fixture.started, 2),
                       'checks': fixture.results}
            if failure:
                summary['failure'] = failure
            args.report.write_text(json.dumps(summary, indent=2) + '\n')
    passed = sum(check['passed'] for check in summary['checks'])
    print(f'{passed}/{len(summary["checks"])} worker isolation checks passed; report: {args.report}', flush=True)
    if not summary['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()

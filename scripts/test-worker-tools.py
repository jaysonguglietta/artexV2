#!/usr/bin/env python3
"""Exercise patched tools and real Chromium in a networkless worker container."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import uuid

EXPECTED_CHECK_COUNT = 8

PROBE = r"""
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const {spawnSync} = require('node:child_process');
const results = [];
const jqBehavior = __ARTEX_JQ_BEHAVIOR__;
const utilLinuxBehavior = __ARTEX_UTIL_LINUX_BEHAVIOR__;
function check(name, fn) { fn(); results.push(name); }
function run(executable, args) {
  const result = spawnSync(executable, args, {encoding:'utf8', timeout:30000});
  assert.equal(result.status, 0, `${executable} failed: ${result.stderr}`);
  return result.stdout.trim();
}
(async () => {
  check('patched Python and native modules', () => {
    const version = run('python3', ['-c', 'import sys,ssl,sqlite3,bz2,lzma,ctypes,dbm.gnu,readline,venv; print(sys.version.split()[0])']);
    assert.equal(version, '3.14.10');
    assert.match(run('python3', ['-m','pip','--version']), /^pip 26\.3\+artex\.1 /);
  });
  check('ordinary Python venv includes patched pip', () => {
    run('python3', ['-m','venv','/workspace/venv']);
    assert.match(run('/workspace/venv/bin/python', ['-m','pip','--version']), /^pip 26\.3\+artex\.1 /);
  });
  check('explicit npm dependency patch', () => {
    assert.equal(run('npm', ['--version']), '12.2.3+artex.1');
    const root = '/usr/local/lib/node_modules/npm/node_modules/';
    for (const [name, version] of [['glob','14.0.6'], ['minimatch','11.0.9']])
      assert.equal(JSON.parse(fs.readFileSync(root+name+'/package.json')).version, version);
  });
  check('assessment tools retained', () => {
    run('/bin/sh', ['-c', 'for tool in bash curl wget git jq unzip rg python3 npm node whois dig nc nmap ip iptables; do command -v "$tool" >/dev/null || exit 1; done']);
    assert.match(run('nmap', ['--version']), /Nmap version/);
  });
  check('jq upstream security and scanning compatibility regressions', () => {
    const output = run('python3', ['-c', jqBehavior]);
    const report = JSON.parse(output.split('\n').at(-1));
    assert.equal(report.passed, true);
    assert.equal(report.checks, 17);
  });
  check('util-linux package integrity and confined assessment behavior', () => {
    const output = run('python3', ['-c', utilLinuxBehavior]);
    const report = JSON.parse(output.split('\n').at(-1));
    assert.equal(report.passed, true);
    assert.equal(report.checks.length, 17);
    assert.ok(report.checks.every(check => check.passed === true));
  });
  check('unused service, virtual display and old Python removed', () => {
    for (const path of ['/usr/bin/pebble','/usr/bin/Xvfb','/usr/lib/python3.14'])
      assert.equal(fs.existsSync(path), false, path);
  });
  const server = http.createServer((_req,res) => {
    res.setHeader('Content-Type','text/html');
    res.end('<title>Worker fixture</title><button id="probe">Verify browser</button><script>document.querySelector("button").onclick=()=>document.body.dataset.result="passed"</script>');
  });
  let browser;
  try {
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    const playwright = require('/opt/artex-browser/node_modules/playwright');
    browser = await playwright.chromium.launch({headless:true, args:['--no-sandbox','--disable-dev-shm-usage']});
    const page = await browser.newPage();
    await page.goto('http://127.0.0.1:'+server.address().port);
    await page.getByRole('button', {name:'Verify browser'}).click();
    assert.equal(await page.locator('body').getAttribute('data-result'), 'passed');
    const png = await page.screenshot();
    assert.deepEqual([...png.subarray(0,8)], [137,80,78,71,13,10,26,10]);
    results.push('real bundled Chromium renders, interacts and captures a PNG');
  } finally {
    if (browser) await browser.close();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
  console.log(JSON.stringify({passed:true, checks:results}));
})().catch(error => { console.error(error); process.exitCode=1; });
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default='artex-local:worker')
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    inspected = subprocess.run(['docker', 'image', 'inspect', args.image],
                               check=True, capture_output=True, text=True, timeout=20)
    images = json.loads(inspected.stdout)
    if len(images) != 1:
        raise ValueError('An exact local worker image is required')
    identity = images[0]['Id']
    name = 'artex-tools-test-' + uuid.uuid4().hex
    try:
        root = Path(__file__).resolve().parents[1]
        behavior_path = root / 'tools/jq/test-behavior.py'
        behavior_bytes = behavior_path.read_bytes()
        util_path = root / 'tools/util-linux/test-behavior.py'
        util_bytes = util_path.read_bytes()
        probe = PROBE.replace('__ARTEX_JQ_BEHAVIOR__', json.dumps(behavior_bytes.decode('utf-8')))
        probe = probe.replace('__ARTEX_UTIL_LINUX_BEHAVIOR__', json.dumps(util_bytes.decode('utf-8')))
        result = subprocess.run([
            'docker', 'run', '--rm', '--name', name,
            '--label', 'artex.purpose=worker-tools-test', '--network', 'none',
            '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
            '--user', '10001:10001', '--memory', '1g', '--cpus', '1', '--pids-limit', '128',
            '--tmpfs', '/workspace:rw,exec,nosuid,nodev,size=512m,uid=10001,gid=10001',
            '--tmpfs', '/tmp:rw,exec,nosuid,nodev,size=128m,uid=10001,gid=10001',
            '-e', 'HOME=/workspace', '--entrypoint', 'node', identity, '-e', probe,
        ], check=True, capture_output=True, text=True, timeout=100)
        report = json.loads(result.stdout)
        if (not isinstance(report, dict) or set(report) != {'passed', 'checks'}
                or report['passed'] is not True or not isinstance(report['checks'], list)
                or len(report['checks']) != EXPECTED_CHECK_COUNT
                or any(not isinstance(check, str) or not check for check in report['checks'])):
            raise ValueError('Incomplete worker tool validation')
        proof = {'schema_version': 1, 'image_id': identity,
                 'platform': images[0]['Os'] + '/' + images[0]['Architecture'],
                 'rootfs_diff_ids': images[0]['RootFS']['Layers'],
                 'source_sha256': {
                     'scripts/test-worker-tools.py': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                     'tools/jq/test-behavior.py': hashlib.sha256(behavior_bytes).hexdigest(),
                     'tools/util-linux/test-behavior.py': hashlib.sha256(util_bytes).hexdigest(),
                 }, 'passed': report['passed'], 'checks': report['checks']}
        if args.report:
            args.report.parent.mkdir(parents=True, exist_ok=True)
            args.report.write_text(json.dumps(proof, indent=2) + '\n')
        print(json.dumps(proof, indent=2))
    finally:
        subprocess.run(['docker', 'rm', '-f', name], capture_output=True, timeout=20)


if __name__ == '__main__':
    try:
        main()
    except subprocess.CalledProcessError as error:
        # The Docker argv embeds complete test programs. Report the bounded
        # failure diagnostic instead of hiding it behind that enormous argv.
        detail = (error.stderr or '').strip()[-8192:]
        raise SystemExit('Worker tool verification failed: ' + (detail or f'exit status {error.returncode}')) from None

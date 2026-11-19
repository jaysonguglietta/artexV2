#!/usr/bin/env python3
"""Exercise patched tools and real Chromium in a networkless worker container."""
import argparse
import json
import subprocess
import uuid


PROBE = r"""
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const {spawnSync} = require('node:child_process');
const results = [];
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
    args = parser.parse_args()
    inspected = subprocess.run(['docker', 'image', 'inspect', args.image],
                               check=True, capture_output=True, text=True, timeout=20)
    images = json.loads(inspected.stdout)
    if len(images) != 1:
        raise ValueError('An exact local worker image is required')
    identity = images[0]['Id']
    name = 'artex-tools-test-' + uuid.uuid4().hex
    try:
        result = subprocess.run([
            'docker', 'run', '--rm', '--name', name,
            '--label', 'artex.purpose=worker-tools-test', '--network', 'none',
            '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
            '--user', '10001:10001', '--memory', '1g', '--cpus', '1', '--pids-limit', '128',
            '--tmpfs', '/workspace:rw,exec,nosuid,nodev,size=512m,uid=10001,gid=10001',
            '--tmpfs', '/tmp:rw,exec,nosuid,nodev,size=128m,uid=10001,gid=10001',
            '-e', 'HOME=/workspace', '--entrypoint', 'node', identity, '-e', PROBE,
        ], check=True, capture_output=True, text=True, timeout=100)
        report = json.loads(result.stdout)
        if report.get('passed') is not True or len(report.get('checks', [])) != 6:
            raise ValueError('Incomplete worker tool validation')
        print(json.dumps({'image_id': identity, **report}, indent=2))
    finally:
        subprocess.run(['docker', 'rm', '-f', name], capture_output=True, timeout=20)


if __name__ == '__main__':
    main()

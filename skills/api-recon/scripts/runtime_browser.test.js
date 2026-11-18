'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

async function serve(t, handler) {
  const server = http.createServer(handler);
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { server.closeAllConnections(); return new Promise(resolve => server.close(resolve)); });
  return `http://127.0.0.1:${server.address().port}`;
}

for (const forward of [false, true]) {
  test(`real browser recon preserves policy with forward=${forward}`, {
    skip: !process.env.CHROMIUM, timeout: 30000,
  }, async t => {
    const counts = { collector: 0, mutation: 0, small: 0, cookie: false };
    const collector = await serve(t, (_req, res) => { counts.collector++; res.end('private fixture'); });
    const origin = await serve(t, (req, res) => {
      if (req.url === '/api/small') {
        counts.small++; counts.cookie = req.headers.cookie?.includes('synthetic=fixture') || false;
        res.setHeader('Content-Type', 'application/json'); res.end('{"ok":true}'); return;
      }
      if (req.url === '/api/mutate') { counts.mutation++; res.end('{}'); return; }
      if (req.url === '/api/redirect') { res.writeHead(302, { Location: collector + '/api/collect' }); res.end(); return; }
      if (req.url === '/api/large') { res.end('x'.repeat(1000)); return; }
      res.setHeader('Content-Type', 'text/html');
      res.end(`<!doctype html><title>Local recon fixture</title><script>
        for (const [url,options] of [
          ['/api/small',{}], ['/api/redirect',{}], ['/api/large',{}],
          ['/api/mutate',{method:'POST',body:'synthetic input'}],
          [${JSON.stringify(collector + '/api/collect')},{}]
        ]) fetch(url,options).catch(()=>{});
      </script>`);
    });
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'artex-recon-browser-'));
    t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
    const config = path.join(dir, 'config.json');
    fs.writeFileSync(config, JSON.stringify({ baseUrl: origin, chromium: process.env.CHROMIUM,
      // The fixture runs inside a networkless, capability-free container.
      noSandbox: process.env.ARTEX_TEST_NO_SANDBOX === '1', forward,
      cookies: [{ name: 'synthetic', value: 'fixture' }], routes: ['/'],
      waitMs: 500, perRouteMs: 300, maxRuntimeMs: 20000, maxResponseBytes: 64,
    }), { mode: 0o600 });
    let output = '';
    const code = await new Promise((resolve, reject) => {
      const child = spawn(process.execPath, [path.join(__dirname, 'runtime_harvest.js'), config], {
        env: { ...process.env, HTTP_PROXY: '', HTTPS_PROXY: '', ALL_PROXY: '' },
      });
      child.stdout.on('data', chunk => { output += chunk; });
      child.stderr.on('data', chunk => { output += chunk; });
      child.on('error', reject); child.on('exit', resolve);
    });
    assert.equal(code, 0, output);
    assert.equal(counts.collector, 0, 'cross-origin or redirect collector was contacted');
    assert.equal(counts.mutation, 0, 'POST reached the real backend');
    assert.equal(counts.small > 0, forward, 'read forwarding ignored its opt-in');
    if (forward) assert.equal(counts.cookie, true, 'same-origin synthetic cookie was not scoped correctly');
    const result = JSON.parse(fs.readFileSync(path.join(dir, 'runtime_api.json'), 'utf8'));
    assert.ok(result.rec.some(entry => entry.m === 'POST' && entry.u === '/api/mutate'));
    assert.ok(result.limitations.includes('Unapproved origin or URL scheme blocked'));
    if (forward) assert.ok(result.rec.some(entry => entry.u === '/api/large' && entry.captureError));
    assert.equal(fs.statSync(path.join(dir, 'runtime_api.json')).mode & 0o777, 0o600);
  });
}

test('coverage preload mocks fetch Request and XHR without backend writes', {
  skip: !process.env.CHROMIUM, timeout: 30000,
}, async t => {
  const puppeteer = require('puppeteer-core');
  let collectorCalls = 0, apiCalls = 0;
  const collector = await serve(t, (_req, res) => { collectorCalls++; res.end('{}'); });
  const origin = await serve(t, (req, res) => {
    if (req.url.startsWith('/api/')) { apiCalls++; res.end('{}'); return; }
    res.setHeader('Content-Type', 'text/html');
    res.end(`<!doctype html><script>
      window.fixture = {fetch:false,xhr:0,blocked:false};
      fetch(new Request('/api/mutate',{method:'POST',body:'synthetic'}))
        .then(r=>r.json()).then(()=>window.fixture.fetch=true);
      const xhr = new XMLHttpRequest(); xhr.open('POST','/api/mutate'); xhr.responseType='json';
      xhr.addEventListener('loadend',()=>{window.fixture.xhr++;window.fixture.json=typeof xhr.response==='object'});
      xhr.send('synthetic');
      fetch(${JSON.stringify(collector + '/api/collect')}).catch(()=>window.fixture.blocked=true);
    </script>`);
  });
  const browser = await puppeteer.launch({ executablePath: process.env.CHROMIUM, headless: true,
    args: process.env.ARTEX_TEST_NO_SANDBOX === '1' ? ['--no-sandbox', '--disable-dev-shm-usage'] : [] });
  t.after(() => browser.close());
  const page = await browser.newPage();
  await page.evaluateOnNewDocument(fs.readFileSync(path.join(__dirname,'preload.js'),'utf8'));
  await page.goto(origin, { waitUntil: 'domcontentloaded' });
  await page.waitForFunction(() => window.fixture?.fetch && window.fixture.xhr && window.fixture.blocked, { timeout: 5000 });
  const result = await page.evaluate(() => ({ fixture: window.fixture, detail: window.__API_RECON_DETAIL__ }));
  assert.equal(apiCalls, 0); assert.equal(collectorCalls, 0);
  assert.equal(result.fixture.xhr, 1, 'XHR completion fired multiple times');
  assert.equal(result.fixture.json, true, 'XHR json response type was lost');
  assert.equal(result.detail.filter(entry => entry.method === 'POST' && new URL(entry.url, origin).pathname === '/api/mutate').length, 2);
});

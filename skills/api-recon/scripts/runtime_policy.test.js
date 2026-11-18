'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const { prepareConfig, requestAllowed, forwardRead } = require('./runtime_policy');

async function localServer(t, handler) {
  const server = http.createServer(handler);
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { server.closeAllConnections(); return new Promise(resolve => server.close(resolve)); });
  return `http://127.0.0.1:${server.address().port}`;
}

test('default mock mode never forwards a request', async () => {
  const cfg = prepareConfig({ baseUrl: 'https://example.com' });
  await assert.rejects(forwardRead(cfg, 'https://example.com/api/data', 'GET'), /opt-in/);
});

test('origin policy rejects alternate API destinations and credential-bearing URLs', async () => {
  const cfg = prepareConfig({ baseUrl: 'https://example.com', forward: true,
    allowedAssetOrigins: ['https://cdn.example.net'] });
  assert.equal(requestAllowed(cfg, 'https://cdn.example.net/static/app.js', 'GET', false), true);
  assert.equal(requestAllowed(cfg, 'https://cdn.example.net/api/data', 'GET', true), false);
  for (const url of ['http://example.com/api', 'https://example.com:8443/api',
    'http://169.254.169.254/latest/?x=/api/', 'file:///etc/passwd', 'https://user:secret@example.com/api', 'https://example.com/api?q=' + 'x'.repeat(9000)]) {
    assert.equal(requestAllowed(cfg, url, 'GET', true), false, url);
    await assert.rejects(forwardRead(cfg, url, 'GET'));
  }
});

test('POST cannot be forwarded even when read forwarding is enabled', async () => {
  const cfg = prepareConfig({ baseUrl: 'https://example.com', forward: true });
  await assert.rejects(forwardRead(cfg, 'https://example.com/api/delete', 'POST'), /GET\/HEAD/);
});

test('redirect cannot reach a collector or forward a cookie', async t => {
  let collectorCalls = 0;
  const collector = await localServer(t, (_req, res) => { collectorCalls++; res.end('secret'); });
  const origin = await localServer(t, (_req, res) => { res.writeHead(302, { Location: collector + '/api/collect' }); res.end(); });
  const cfg = prepareConfig({ baseUrl: origin, forward: true });
  await assert.rejects(forwardRead(cfg, origin + '/api/redirect', 'GET', { cookie: 'synthetic=fixture' }));
  assert.equal(collectorCalls, 0);
});

test('normal same-origin reads work and streamed bodies are bounded', async t => {
  const origin = await localServer(t, (req, res) => {
    if (req.url === '/api/small') { res.end('{"ok":true}'); return; }
    res.write('x'.repeat(40)); res.end('y'.repeat(40));
  });
  const cfg = prepareConfig({ baseUrl: origin, forward: true, maxResponseBytes: 64 });
  assert.equal((await forwardRead(cfg, origin + '/api/small', 'GET')).text, '{"ok":true}');
  await assert.rejects(forwardRead(cfg, origin + '/api/large', 'GET'), /capture limit/);
});

test('stalled requests time out instead of hanging the harvester', async t => {
  const origin = await localServer(t, () => {});
  const cfg = prepareConfig({ baseUrl: origin, forward: true, fetchTimeoutMs: 40 });
  await assert.rejects(forwardRead(cfg, origin + '/api/stalled', 'GET'), { name: 'TimeoutError' });
});

test('a configured proxy cannot be silently bypassed by Node forwarding', async t => {
  let calls = 0;
  const origin = await localServer(t, (_req, res) => { calls++; res.end('{}'); });
  const cfg = prepareConfig({ baseUrl: origin, forward: true, proxy: 'http://127.0.0.1:9' });
  await assert.rejects(forwardRead(cfg, origin + '/api/data', 'GET'), /configured proxy/);
  assert.equal(calls, 0);
});

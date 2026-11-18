'use strict';

// Network limits for the standalone recon helper. Browser hooks are not a full
// network sandbox (for example WebSockets); an external egress policy is required.
const SAFE_METHODS = new Set(['GET', 'HEAD']);

function originOf(value) {
  const url = new URL(value);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
    throw new Error('Only HTTP(S) URLs without embedded credentials are permitted');
  }
  return url.origin;
}

function limit(value, fallback, maximum) {
  if (value === undefined) return fallback;
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) {
    throw new Error(`Limit must be an integer between 1 and ${maximum}`);
  }
  return value;
}

function prepareConfig(input) {
  const cfg = { ...input };
  cfg.origin = originOf(cfg.baseUrl);
  cfg.forward = cfg.forward === true;
  cfg.allowedAssetOrigins = new Set((cfg.allowedAssetOrigins || []).map(originOf));
  cfg.maxResponseBytes = limit(cfg.maxResponseBytes, 1024 * 1024, 8 * 1024 * 1024);
  cfg.fetchTimeoutMs = limit(cfg.fetchTimeoutMs, 10000, 30000);
  cfg.respMax = limit(cfg.respMax, 600, 8192);
  cfg.maxUrlLength = limit(cfg.maxUrlLength, 8192, 65536);
  cfg.maxRequests = limit(cfg.maxRequests, 2000, 5000);
  cfg.maxFrames = limit(cfg.maxFrames, 1000, 5000);
  cfg.maxRoutes = limit(cfg.maxRoutes, 100, 500);
  cfg.maxRuntimeMs = limit(cfg.maxRuntimeMs, 300000, 600000);
  return cfg;
}

function requestAllowed(cfg, value, method, isApi) {
  if (typeof value !== "string" || value.length > cfg.maxUrlLength) return false;
  let url;
  try { url = new URL(value); } catch { return false; }
  if (['data:', 'blob:', 'about:'].includes(url.protocol)) return !isApi;
  let origin;
  try { origin = originOf(value); } catch { return false; }
  if (origin === cfg.origin) return true;
  // Extra origins may serve explicitly allowed static resources only. API data
  // and configured cookies are never forwarded to a secondary origin.
  return !isApi && SAFE_METHODS.has(method) && cfg.allowedAssetOrigins.has(origin);
}

async function readBounded(response, maxBytes) {
  const length = response.headers.get('content-length');
  if (length !== null && Number(length) > maxBytes) {
    await response.body?.cancel();
    throw new Error('Response exceeds the capture limit');
  }
  if (!response.body) return '';
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxBytes) {
        await reader.cancel();
        throw new Error('Response exceeds the capture limit');
      }
      chunks.push(Buffer.from(value));
    }
    return Buffer.concat(chunks, size).toString('utf8');
  } finally {
    reader.releaseLock();
  }
}

async function forwardRead(cfg, url, method, requestHeaders = {}) {
  if (cfg.proxy) throw new Error('Read forwarding cannot honor the configured proxy; keep mock mode enabled');
  if (!cfg.forward || !SAFE_METHODS.has(method) || !requestAllowed(cfg, url, method, true)) {
    throw new Error('Forwarding requires explicit opt-in, the base origin, and GET/HEAD');
  }
  const headers = { ...requestHeaders };
  for (const key of Object.keys(headers)) {
    if (['host', 'connection', 'content-length', 'transfer-encoding', 'upgrade', 'proxy-authorization'].includes(key.toLowerCase())) {
      delete headers[key];
    }
  }
  const response = await fetch(url, {
    method, headers, redirect: 'error', signal: AbortSignal.timeout(cfg.fetchTimeoutMs),
  });
  return { text: await readBounded(response, cfg.maxResponseBytes), status: response.status,
    contentType: response.headers.get('content-type') || '' };
}

module.exports = { prepareConfig, requestAllowed, forwardRead, SAFE_METHODS };

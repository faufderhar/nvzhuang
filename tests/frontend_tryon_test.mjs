import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const html = fs.readFileSync(new URL('../index.html', import.meta.url), 'utf8');

const inlineScripts = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi)];
assert.ok(inlineScripts.length > 0, 'no inline scripts found');
for (const [index, match] of inlineScripts.entries()) {
  new vm.Script(match[1], {filename: `index.html:inline-${index + 1}`});
}

function extractFunction(name) {
  const patterns = [`function ${name}(`, `async function ${name}(`];
  const start = patterns.map(pattern => html.indexOf(pattern)).find(index => index >= 0);
  assert.notEqual(start, undefined, `missing function ${name}`);
  const bodyStart = html.indexOf('{', start);
  let depth = 0;
  let quote = '';
  let escaped = false;
  for (let i = bodyStart; i < html.length; i++) {
    const char = html[i];
    if (quote) {
      if (escaped) escaped = false;
      else if (char === '\\') escaped = true;
      else if (char === quote) quote = '';
      continue;
    }
    if (char === "'" || char === '"' || char === '`') {
      quote = char;
      continue;
    }
    if (char === '{') depth++;
    if (char === '}') {
      depth--;
      if (depth === 0) return html.slice(start, i + 1);
    }
  }
  throw new Error(`unterminated function ${name}`);
}

const helperNames = [
  'detectImageMimeFromBytes',
  'detectImageMimeFromBase64',
  'normalizeImageMime',
  'imageExtensionForMime',
  'resolveResultImageInfo',
  'createResultImageDataUrl',
  'parseTryonResponse',
  'getErrorMessageFromResponse',
];

const translations = {
  aiFailed: 'failed',
  aiGatewayErr: 'gateway',
  aiInvalidResponse: 'invalid response',
  aiHttpErr: 'http error',
  errAuthRequired: 'auth required',
  errAuthInvalid: 'auth invalid',
  errOriginNotAllowed: 'origin denied',
  errRequestTooLarge: 'too large',
  errInvalidImageBase64: 'invalid image',
  errUnsupportedImageType: 'unsupported image',
  errImageFileTooLarge: 'image too large',
  errImageDimensionsInvalid: 'bad dimensions',
  errImagePixelLimitExceeded: 'too many pixels',
  errRateLimited: 'rate limited',
  errDailyQuotaExceeded: 'quota exceeded',
  errConcurrencyLimited: 'concurrency limited',
  settingsBackendOutdated: 'backend outdated',
};

const context = {
  atob,
  Uint8Array,
  tryonSettings: {useCustom: false},
  t: key => translations[key] || key,
};
vm.createContext(context);
vm.runInContext(helperNames.map(extractFunction).join('\n'), context);

const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB';
const jpeg = '/9j/4AAQSkZJRgABAQAAAQABAAD';
const webp = 'UklGRgQAAABXRUJQ';

assert.equal(context.detectImageMimeFromBase64(png), 'image/png');
assert.equal(context.detectImageMimeFromBase64(jpeg), 'image/jpeg');
assert.equal(context.detectImageMimeFromBase64(webp), 'image/webp');
assert.equal(context.imageExtensionForMime('image/jpeg'), 'jpg');
assert.equal(context.imageExtensionForMime('image/webp'), 'webp');

const resolved = context.resolveResultImageInfo(jpeg, 'image/png');
assert.equal(resolved.mime, 'image/jpeg', 'magic bytes must win over a mismatched declaration');
assert.equal(resolved.extension, 'jpg');
assert.match(context.createResultImageDataUrl(resolved), /^data:image\/jpeg;base64,/);

assert.throws(() => context.resolveResultImageInfo('cGxhaW4gdGV4dA==', ''), /unsupported/i);

function fakeResponse({status, contentType, jsonValue, jsonError}) {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: {get: name => name.toLowerCase() === 'content-type' ? contentType : null},
    json: async () => {
      if (jsonError) throw jsonError;
      return jsonValue;
    },
  };
}

assert.equal(await context.parseTryonResponse(fakeResponse({status: 502, contentType: 'text/html'})), null);
assert.equal(await context.parseTryonResponse(fakeResponse({status: 200, contentType: 'application/json', jsonError: new Error('bad json')})), null);
assert.deepEqual(
  await context.parseTryonResponse(fakeResponse({status: 400, contentType: 'application/problem+json', jsonValue: {message: 'bad'}})),
  {message: 'bad'},
);

assert.equal(context.getErrorMessageFromResponse({status: 413, ok: false}, null).code, 'REQUEST_TOO_LARGE');
assert.equal(context.getErrorMessageFromResponse({status: 401, ok: false}, null).code, 'AUTH_REQUIRED');
assert.equal(context.getErrorMessageFromResponse({status: 403, ok: false}, null).code, 'ORIGIN_NOT_ALLOWED');
assert.match(context.getErrorMessageFromResponse({status: 502, ok: false}, null).text, /502/);
assert.match(context.getErrorMessageFromResponse({status: 200, ok: true}, null).text, /invalid response/);

// /comfyui/health 已纳入后端鉴权：设置页“测试连接”必须携带后端 Token
{
  const start = html.indexOf("settingsComfyTest.addEventListener('click'");
  assert.ok(start >= 0, 'missing ComfyUI health handler');
  const handler = html.slice(start, html.indexOf('settingsForm.addEventListener', start));
  assert.match(handler, /settingsBackendToken\.value/, 'health check must read backend token');
  assert.match(handler, /\['Authorization'\]\s*=\s*'Bearer '/, 'health check must send Authorization header');
  assert.match(handler, /fetch\(backendUrl \+ '\/comfyui\/health', \{headers:healthHeaders\}\)/);
}

// 后端上游失败返回 502 JSON 固定文案（F-03/F-24）：前端应展示该文案，而不是通用网关错误
{
  const msg = context.getErrorMessageFromResponse({status: 502, ok: false},
    {code: 50201, message: '换装生成失败，请稍后重试', data: {error_code: 'UPSTREAM_FAILED', request_id: 'r1'}});
  assert.match(msg.text, /换装生成失败/);
}

// F-17：内容安全策略
{
  const csp = html.match(/<meta http-equiv="Content-Security-Policy" content="([^"]+)">/);
  assert.ok(csp, 'missing CSP meta');
  const policy = csp[1];
  for (const directive of ["default-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-src 'none'"]) {
    assert.ok(policy.includes(directive), `CSP missing ${directive}`);
  }
  assert.ok(!policy.includes("'unsafe-eval'"), 'CSP must not allow eval');
  assert.match(policy, /connect-src [^;]*https:/, 'CSP must allow https backends');
  assert.match(policy, /connect-src [^;]*http:\/\/127\.0\.0\.1:\*/, 'CSP must allow loopback backend');
  assert.match(policy, /script-src 'self' 'unsafe-inline';/, 'no third-party script origins allowed');
  assert.ok(html.indexOf('Content-Security-Policy') < html.indexOf('<script'), 'CSP must precede first script');
}

// F-17：“仅本次会话”模式只写 sessionStorage，并清除长期存储中的旧副本
{
  function fakeStorage() {
    const map = new Map();
    return {
      map,
      getItem: k => (map.has(k) ? map.get(k) : null),
      setItem: (k, v) => map.set(k, String(v)),
      removeItem: k => map.delete(k),
    };
  }
  const storageCtx = {localStorage: fakeStorage(), sessionStorage: fakeStorage(), SETTINGS_KEY: 'k', JSON};
  vm.createContext(storageCtx);
  vm.runInContext(['readStoredSettings', 'persistSettings', 'clearStoredSettings'].map(extractFunction).join('\n'), storageCtx);

  storageCtx.persistSettings({apiKey: 'long-term', sessionOnly: false});
  assert.ok(storageCtx.localStorage.map.has('k'));
  storageCtx.persistSettings({apiKey: 'session-secret', sessionOnly: true});
  assert.ok(!storageCtx.localStorage.map.has('k'), 'session-only must remove persistent copy');
  assert.match(storageCtx.sessionStorage.getItem('k'), /session-secret/);
  assert.match(storageCtx.readStoredSettings(), /session-secret/);
  storageCtx.clearStoredSettings();
  assert.equal(storageCtx.readStoredSettings(), null);
}

console.log('frontend try-on tests passed');

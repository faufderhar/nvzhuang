// 前端小改进回归测试（O-03 生成记录保留、O-12 错误与 Retry-After、O-13 语言记忆、O-24 config.js）
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const html = fs.readFileSync(new URL('../index.html', import.meta.url), 'utf8');
const configJs = fs.readFileSync(new URL('../config.js', import.meta.url), 'utf8');

function balancedFrom(start) {
  const bodyStart = html.indexOf('{', start);
  let depth = 0, quote = '', escaped = false;
  for (let i = bodyStart; i < html.length; i++) {
    const c = html[i];
    if (quote) { if (escaped) escaped = false; else if (c === '\\') escaped = true; else if (c === quote) quote = ''; continue; }
    if (c === "'" || c === '"' || c === '`') { quote = c; continue; }
    if (c === '{') depth++;
    if (c === '}' && --depth === 0) return html.slice(start, i + 1);
  }
  throw new Error('unterminated');
}
const fn = name => {
  const i = html.indexOf(`function ${name}(`);
  assert.ok(i >= 0, `missing ${name}`);
  const asyncStart = html.slice(Math.max(0, i - 6), i) === 'async ' ? i - 6 : i;
  return balancedFrom(asyncStart);
};
const constLine = name => {
  const m = html.match(new RegExp(`^\\s*const ${name} = [^\\n]*;$`, 'm'));
  assert.ok(m, `missing const ${name}`);
  return m[0];
};
const run = (code, globals = {}) => {
  const ctx = { console: { warn() {} }, URL, Date, Number, Math, String, Boolean, Promise, ...globals };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
};

// ---------- O-12 Retry-After 与错误映射 ----------
{
  const zh = {
    errRateLimited: '请求过于频繁', errConcurrencyLimited: '当前生成任务较多', errDailyQuotaExceeded: '今日生成额度已用完，请明天再试',
    aiGatewayTimeout: '生成超时', aiGatewayErr: '网关不可用', aiHttpErr: '换装服务返回异常', aiInvalidResponse: '无法识别', aiFailed: '换装失败',
    errRequestTooLarge: '请求过大',
  };
  const ctx = run([
    'const tryonSettings = { useCustom: false };',
    'function t(k){ return DICT[k] || k; }',
    fn('parseRetryAfter'), fn('withRetry'), fn('getErrorMessageFromResponse'),
    'globalThis.api = { parseRetryAfter, getErrorMessageFromResponse };',
  ].join('\n'), { DICT: zh });
  const { parseRetryAfter, getErrorMessageFromResponse } = ctx.api;
  assert.equal(parseRetryAfter('5'), 5);
  assert.equal(parseRetryAfter(''), 0);
  assert.equal(parseRetryAfter('0'), 0);
  assert.equal(parseRetryAfter('abc'), 0);
  assert.equal(parseRetryAfter('99999'), 3600);
  const now = Date.parse('2026-01-01T00:00:00Z');
  assert.equal(parseRetryAfter('Thu, 01 Jan 2026 00:00:10 GMT', now), 10);
  assert.equal(parseRetryAfter('Thu, 01 Jan 2025 00:00:10 GMT', now), 0);

  const resp = (status, retry) => ({ status, ok: false, headers: { get: k => (k === 'retry-after' ? retry : null) } });
  // 网关直接返回的非 JSON 429
  let info = getErrorMessageFromResponse(resp(429, '7'), null);
  assert.equal(info.code, 'RATE_LIMITED');
  assert.equal(info.retryAfter, 7);
  // 后端并发 429 + Retry-After
  info = getErrorMessageFromResponse(resp(429, '5'), { code: 42903, data: { error_code: 'CONCURRENCY_LIMITED' } });
  assert.equal(info.code, 'CONCURRENCY_LIMITED');
  assert.equal(info.retryAfter, 5);
  // 每日额度：提示明天再试，不倒计时
  info = getErrorMessageFromResponse(resp(429, '5'), { code: 42902, data: { error_code: 'DAILY_QUOTA_EXCEEDED' } });
  assert.equal(info.code, 'DAILY_QUOTA_EXCEEDED');
  assert.ok(info.text.includes('明天'));
  assert.equal(info.retryAfter, undefined);
  // 无 Retry-After 时不带倒计时
  assert.equal(getErrorMessageFromResponse(resp(429, null), null).retryAfter, undefined);
  // 非 JSON 504 / 502 / 413
  assert.equal(getErrorMessageFromResponse(resp(504), null).code, 'GATEWAY_TIMEOUT');
  assert.equal(getErrorMessageFromResponse(resp(502), null).code, 'GATEWAY_ERROR');
  assert.equal(getErrorMessageFromResponse(resp(413), null).code, 'REQUEST_TOO_LARGE');
  // 生产 Nginx JSON 错误页：直接读取仓库内的 deploy/nginx/errors/*.json，确保前后端约定一致
  const nginxPages = { 413: 'REQUEST_TOO_LARGE', 429: 'RATE_LIMITED', 502: 'GATEWAY_ERROR', 504: 'GATEWAY_TIMEOUT' };
  for (const [status, want] of Object.entries(nginxPages)) {
    const body = JSON.parse(fs.readFileSync(new URL(`../deploy/nginx/errors/${status}.json`, import.meta.url), 'utf8'));
    assert.equal(body.data.error_code, want, `${status}.json error_code`);
    const got = getErrorMessageFromResponse(resp(Number(status), status === '429' ? '10' : null), body);
    assert.equal(got.code, want, `nginx ${status}.json 映射`);
    assert.ok(!got.text.includes(body.message), `nginx ${status}.json 不应拼接网关原文`);
    if (status === '429') assert.equal(got.retryAfter, 10);
  }
  assert.match(getErrorMessageFromResponse(resp(503), { code: 50200, message: 'x', data: { error_code: 'GATEWAY_ERROR' } }).text, /HTTP 503/);
}

// i18n 新文案中英齐全
for (const key of ['retryAfterSuffix', 'retryCountdown', 'retryReady', 'aiGatewayTimeout', 'errDailyQuotaExceeded']) {
  const n = (html.match(new RegExp(`\\b${key}:`, 'g')) || []).length;
  assert.equal(n, 2, `${key} 应在 zh/en 各出现一次，实际 ${n}`);
}

// ---------- O-13 语言记忆 ----------
{
  const ctx = run(fn('resolveInitialLang') + '\nglobalThis.f = resolveInitialLang;');
  const f = ctx.f;
  assert.equal(f('en', 'zh'), 'en', '?lang 优先');
  assert.equal(f(null, 'en'), 'en', '其次本机保存');
  assert.equal(f('xx', 'yy'), 'zh', '非法值回退中文');
  assert.equal(f(null, ''), 'zh');
  assert.ok(html.includes("const LANG_STORAGE_KEY = 'nvzhuang_lang';"));
}

// ---------- O-03 生成记录保留 ----------
{
  const code = [
    ...['HISTORY_MAX_RECORDS', 'HISTORY_PRESSURE_RATIO', 'HISTORY_PRESSURE_KEEP', 'HISTORY_QUOTA_KEEP'].map(constLine),
    fn('isQuotaExceededError'), fn('saveRecordWithRetention'),
    'globalThis.api = { saveRecordWithRetention, isQuotaExceededError, MAX: HISTORY_MAX_RECORDS };',
  ].join('\n');
  const { saveRecordWithRetention, isQuotaExceededError, MAX } = run(code).api;
  assert.equal(MAX, 50);
  assert.ok(isQuotaExceededError({ name: 'QuotaExceededError' }));
  assert.ok(isQuotaExceededError({ code: 22 }));
  assert.ok(!isQuotaExceededError(new Error('x')));

  const mkOps = ({ failPut = [], estimate = null } = {}) => {
    const log = [];
    let n = 0;
    return {
      log,
      put: async r => { const e = failPut[n++]; if (e) throw e; log.push(['put', r.id]); },
      prune: async k => { log.push(['prune', k]); },
      estimate: estimate ? async () => estimate : null,
    };
  };
  // 正常：写入后裁剪到 50 条
  let ops = mkOps();
  await saveRecordWithRetention({ id: 'a' }, ops);
  assert.deepEqual(ops.log, [['put', 'a'], ['prune', 50]]);
  // 存储压力 > 90%：先收缩到 25 条
  ops = mkOps({ estimate: { usage: 95, quota: 100 } });
  await saveRecordWithRetention({ id: 'b' }, ops);
  assert.deepEqual(ops.log, [['prune', 25], ['put', 'b'], ['prune', 50]]);
  // 配额错误：收缩到 10 条后重试一次
  const quotaErr = Object.assign(new Error('full'), { name: 'QuotaExceededError' });
  ops = mkOps({ failPut: [quotaErr] });
  await saveRecordWithRetention({ id: 'c' }, ops);
  assert.deepEqual(ops.log, [['prune', 10], ['put', 'c'], ['prune', 50]]);
  // 其他错误直接抛出，不删除记录
  ops = mkOps({ failPut: [new Error('other')] });
  await assert.rejects(saveRecordWithRetention({ id: 'd' }, ops), /other/);
  assert.deepEqual(ops.log, []);
  // 重试仍失败则抛出
  ops = mkOps({ failPut: [quotaErr, quotaErr] });
  await assert.rejects(saveRecordWithRetention({ id: 'e' }, ops));

  // pruneHistoryRecords：按 createdAt 从旧到新删除
  const start = html.indexOf('const pruneHistoryRecords = keep=>');
  const end = html.indexOf('\n  });', start) + 6;
  const pruneSrc = html.slice(start, end);
  const records = Array.from({ length: 55 }, (_, i) => ({ id: 'r' + i, createdAt: 1000 + ((i * 7) % 55) }));
  const fakeStore = {
    count() { const req = {}; queueMicrotask(() => { req.result = records.length; req.onsuccess && req.onsuccess(); }); return req; },
    index() {
      return {
        openCursor() {
          const req = {};
          const sorted = [...records].sort((a, b) => a.createdAt - b.createdAt);
          let i = 0;
          const step = () => queueMicrotask(() => {
            const rec = sorted[i];
            req.result = rec ? {
              delete() { records.splice(records.indexOf(rec), 1); },
              continue() { i++; step(); },
            } : null;
            req.onsuccess && req.onsuccess();
          });
          step();
          return req;
        },
      };
    },
  };
  const ctx = run(pruneSrc + '\nglobalThis.prune = pruneHistoryRecords;', {
    queueMicrotask,
    historyRequest: async (mode, op) => { op(fakeStore); await new Promise(r => setTimeout(r, 20)); },
  });
  await ctx.prune(50);
  assert.equal(records.length, 50);
  assert.ok(Math.min(...records.map(r => r.createdAt)) === 1005, '应删除最旧的 5 条');
}

// ---------- O-24 config.js ----------
{
  const nonComment = configJs.split('\n').filter(l => l.trim() && !l.trim().startsWith('//'));
  assert.deepEqual(nonComment, [], 'config.js 默认不得设置任何值');
  assert.ok(!/sk-|AIza|Bearer/.test(configJs.replace(/^\/\/.*$/gm, '')));
  const cfgIdx = html.indexOf('<script src="config.js"></script>');
  assert.ok(cfgIdx > 0 && cfgIdx < html.indexOf('<script src="data.js"></script>'), 'config.js 应在 data.js 之前加载');
  // CSP 未放宽
  assert.ok(html.includes("script-src 'self' 'unsafe-inline';"));

  const ctx = run([fn('isLoopbackHost'), fn('sanitizeConfiguredApi'), 'globalThis.f = sanitizeConfiguredApi;'].join('\n'),
    { location: { origin: 'https://site.example', hostname: 'site.example' } });
  const f = ctx.f;
  assert.equal(f(undefined), '');
  assert.equal(f(''), '');
  assert.equal(f('https://tryon.example.com/'), 'https://tryon.example.com');
  assert.equal(f('http://127.0.0.1:8000'), 'http://127.0.0.1:8000');
  assert.equal(f('http://tryon.example.com'), '', '远程 HTTP 必须拒绝');
  assert.equal(f('javascript:alert(1)'), '');
  assert.equal(f('https://user:pass@tryon.example.com'), '', '禁止 URL 内嵌凭据');
}

console.log('frontend small-wins tests passed');

// CSP 回归测试：script-src 不允许 'unsafe-inline'，内联脚本必须由哈希放行且与当前内容一致
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { currentScriptSrc, expectedScriptSrc, inlineScriptHashes } from '../scripts/csp-hashes.mjs';

const html = fs.readFileSync(new URL('../index.html', import.meta.url), 'utf8');
const scriptSrc = currentScriptSrc(html);
assert.ok(!scriptSrc.includes("'unsafe-inline'"), `script-src 不应包含 'unsafe-inline': ${scriptSrc}`);
assert.ok(!scriptSrc.includes("'unsafe-eval'"), `script-src 不应包含 'unsafe-eval': ${scriptSrc}`);
assert.ok(inlineScriptHashes(html).length > 0, '未找到内联脚本');
assert.equal(scriptSrc, expectedScriptSrc(html), 'CSP 哈希过期：修改内联脚本后请运行 node scripts/csp-hashes.mjs --write');
// 哈希只能放行脚本元素本身，不能放行 HTML 事件处理属性（onclick= 等）
assert.ok(!/<[a-z][^>]*\son[a-z]+\s*=\s*["']/i.test(html.replace(/<script[\s\S]*?<\/script>/gi, '')), '不允许使用内联事件处理属性');
const csp = html.match(/<meta http-equiv="Content-Security-Policy" content="([^"]*)">/)[1];
for (const must of ["object-src 'none'", "base-uri 'none'", "frame-src 'none'"]) {
  assert.ok(csp.includes(must), `CSP 缺少 ${must}`);
}
console.log('csp tests passed');

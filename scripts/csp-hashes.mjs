// 维护 index.html 中 CSP 的 script-src 内联脚本哈希（审计：去掉 script-src 'unsafe-inline'）。
//   node scripts/csp-hashes.mjs          校验：CSP 中的哈希与当前内联脚本一致，不一致时以非零状态退出
//   node scripts/csp-hashes.mjs --write  修改内联脚本后执行，重新计算并写回 CSP
// 只在本机读写仓库文件，不访问网络。
import crypto from 'node:crypto';
import fs from 'node:fs';

const file = new URL('../index.html', import.meta.url);

export function inlineScriptHashes(html) {
  return [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi)]
    .map(m => `'sha256-${crypto.createHash('sha256').update(m[1], 'utf8').digest('base64')}'`);
}

export function expectedScriptSrc(html) {
  return ["'self'", ...inlineScriptHashes(html)].join(' ');
}

export function currentScriptSrc(html) {
  const csp = html.match(/<meta http-equiv="Content-Security-Policy" content="([^"]*)">/);
  if (!csp) throw new Error('index.html 缺少 CSP meta');
  const dir = csp[1].split(';').map(s => s.trim()).find(s => s.startsWith('script-src '));
  if (!dir) throw new Error('CSP 缺少 script-src');
  return dir.slice('script-src '.length);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const html = fs.readFileSync(file, 'utf8');
  const want = expectedScriptSrc(html);
  const have = currentScriptSrc(html);
  if (process.argv.includes('--write')) {
    if (have !== want) {
      fs.writeFileSync(file, html.replace(`script-src ${have};`, `script-src ${want};`));
      console.log(`已更新 script-src: ${want}`);
    } else {
      console.log('script-src 已是最新');
    }
  } else if (have !== want) {
    console.error(`CSP script-src 与内联脚本不一致，请运行 node scripts/csp-hashes.mjs --write\n  当前: ${have}\n  期望: ${want}`);
    process.exit(1);
  } else {
    console.log('CSP script-src 哈希一致');
  }
}

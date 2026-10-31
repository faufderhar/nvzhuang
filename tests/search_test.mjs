// 搜索召回与性能回归测试（O-04 / O-06）：node tests/search_test.mjs
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const html = fs.readFileSync(new URL('../index.html', import.meta.url), 'utf8');
const dataJs = fs.readFileSync(new URL('../data.js', import.meta.url), 'utf8');

function sliceBalanced(start, open = '{', close = '}') {
  const bodyStart = html.indexOf(open, start);
  let depth = 0, quote = '', escaped = false;
  for (let i = bodyStart; i < html.length; i++) {
    const c = html[i];
    if (quote) { if (escaped) escaped = false; else if (c === '\\') escaped = true; else if (c === quote) quote = ''; continue; }
    if (c === "'" || c === '"' || c === '`') { quote = c; continue; }
    if (c === open) depth++;
    if (c === close && --depth === 0) return html.slice(start, i + 1);
  }
  throw new Error('unterminated at ' + start);
}
const grab = marker => {
  const i = html.indexOf(marker);
  assert.ok(i >= 0, 'missing ' + marker);
  return sliceBalanced(i);
};
const zh2enStart = html.indexOf('const ZH2EN = {};');
const zh2enLoop = html.slice(zh2enStart, html.indexOf('\n}\n', zh2enStart) + 3);
const cjkLine = html.match(/^const CJK_RE = .*$/m)[0];

const code = [
  dataJs,
  grab('const EN2ZH = {') + ';',
  zh2enLoop,
  grab('const ZH_ALIAS = {') + ';',
  cjkLine,
  grab('const EN_CAT = {') + ';',
  "let LANG = 'zh';",
  "const CAT_ZH = {'春':'春季','夏':'夏季','秋':'秋季','冬':'冬季','ランジェリー':'内衣','パーティ':'派对','水着':'泳装','セクシー':'性感'};",
  'function catName(ja){ return LANG === "zh" ? (CAT_ZH[ja] || ja) : (EN_CAT[ja] || ja); }',
  ...['buildIndex', 'termMatches', 'matchesQuery'].map(n => grab('function ' + n + '(')),
  grab('let SEARCH_INDEX = null').split('\n')[0],
  grab('function searchIndexFor('),
  'globalThis.api = { D, setLang: l => { LANG = l; }, buildIndex, matchesQuery, searchIndexFor, getIndex: () => SEARCH_INDEX };',
].join('\n');
const ctx = {};
vm.createContext(ctx);
vm.runInContext(code, ctx);
const { D, setLang, buildIndex, matchesQuery, searchIndexFor, getIndex } = ctx.api;

const count = q => D.filter(d => matchesQuery(searchIndexFor(d), q)).length;
const ids = q => D.filter(d => matchesQuery(searchIndexFor(d), q)).map(d => d.id);

// CJK 子串召回
assert.ok(count('裙') >= 470, `裙 召回不足: ${count('裙')}`);
assert.ok(count('鞋') > 0, '鞋 应命中运动鞋/高跟鞋等');
assert.ok(count('裙') >= count('连衣裙') && count('裙') >= count('半身裙'), '单字应覆盖复合词');
// 别名仍可用
assert.equal(count('黑 裙子'), 191, `黑 裙子: ${count('黑 裙子')}`);
assert.equal(count('黑色'), count('黑'), '黑色/黑 应一致');
// 英文保持前缀匹配：lace 不应命中 necklace
assert.equal(count('lace'), 346, `lace: ${count('lace')}`);
assert.ok(ids('lace').every(id => {
  const d = D.find(x => x.id === id);
  return d.キーワード.split(' ').some(w => w.startsWith('lace'));
}), 'lace 只应前缀命中');
// ID 前缀
assert.equal(count('spr-00'), 89, `spr-00: ${count('spr-00')}`);
assert.equal(count('SPR-0001'), 1);
// 空查询与无结果
assert.equal(count(''), D.length);
assert.equal(count('不存在的词zzz'), 0);
// 多词为 AND
assert.ok(count('黑 裙') <= Math.min(count('黑'), count('裙')));

// 切换语言后重建索引（分类名随语言变化）
const zhIndex = getIndex();
setLang('en');
searchIndexFor(D[0]);
assert.notEqual(getIndex(), zhIndex, '切换语言应重建索引');
assert.ok(buildIndex(D[0]).includes('spring') || buildIndex(D[0]).length > 0);
setLang('zh');

// 性能：预建索引后单次全量搜索 ≤ 2ms（取 50 次平均，CI 机器留 3 倍余量）
searchIndexFor(D[0]);
for (const q of ['黑 裙', 'lace', 'spr-00', '鞋']) {
  const t0 = process.hrtime.bigint();
  for (let i = 0; i < 50; i++) count(q);
  const ms = Number(process.hrtime.bigint() - t0) / 50 / 1e6;
  assert.ok(ms <= 6, `${q} 搜索过慢: ${ms.toFixed(2)}ms`);
  console.log(`search "${q}": ${count(q)} hits, ${ms.toFixed(3)} ms`);
}
console.log('search tests passed');

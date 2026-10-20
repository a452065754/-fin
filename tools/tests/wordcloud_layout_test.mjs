// Layout test for the word cloud renderer in web/app.js.
//
// The layout function is plain browser JavaScript, so it is extracted and
// exercised here with Node. Run it directly:
//
//   node tools/tests/wordcloud_layout_test.mjs
//
// It is also part of the pre-commit check in docs/开发.md.
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const here = path.dirname(fileURLToPath(import.meta.url));
const appJs = fs.readFileSync(path.join(here, '..', '..', 'web', 'app.js'), 'utf-8');

const match = appJs.match(/function layoutWordCloud\(words, sizes\) \{[\s\S]*?\n  \}\n/);
if (!match) {
  console.error('未在 web/app.js 中找到 layoutWordCloud');
  process.exit(1);
}
const layout = new Function('PALETTE', match[0] + '; return layoutWordCloud;')(['#2563eb']);

// Mirrors the size calculation in chartOption.
function sizesFor(words) {
  const maxV = words[0].value || 1;
  const minV = words[words.length - 1].value || 0;
  const span = (maxV - minV) || 1;
  return words.map(w => {
    let r = (w.value - minV) / span;
    if (!isFinite(r) || r < 0) r = 0;
    if (r > 1) r = 1;
    return Math.max(10, Math.min(48, 12 + r * 34));
  });
}

function boxOf(p) {
  let wide = 0;
  for (const ch of p.name) wide += ch.charCodeAt(0) > 0x2e80 ? 1 : 0.55;
  return { w: wide * p.size * 0.62, h: p.size * 1.2 };
}

let failures = 0;
for (const n of [10, 40, 80]) {
  const words = [];
  for (let i = 0; i < n; i++) words.push({ name: '词' + i, value: 100 - i });
  words.sort((a, b) => b.value - a.value);
  const top = words.slice(0, 80);
  const placed = layout(top, sizesFor(top));
  const boxes = placed.map(boxOf);

  let overlaps = 0;
  for (let i = 0; i < placed.length; i++) {
    for (let j = i + 1; j < placed.length; j++) {
      const ox = Math.abs(placed[i].x - placed[j].x) < (boxes[i].w + boxes[j].w) / 2;
      const oy = Math.abs(placed[i].y - placed[j].y) < (boxes[i].h + boxes[j].h) / 2;
      if (ox && oy) overlaps++;
    }
  }
  const outside = placed.filter(p => p.x < 0 || p.x > 520).length;
  const badSize = placed.filter(p => !isFinite(p.size) || p.size <= 0).length;

  const ok = placed.length === top.length && overlaps === 0 &&
             outside === 0 && badSize === 0;
  if (!ok) failures++;
  console.log(
    (ok ? 'PASS' : 'FAIL') +
    ` n=${n} 放置=${placed.length}/${top.length}` +
    ` 重叠=${overlaps} 越界=${outside} 非法字号=${badSize}` +
    ` 高度=${Math.round(placed.totalHeight || 0)}`
  );
}

if (failures > 0) {
  console.error(`词云布局测试失败：${failures} 项`);
  process.exit(1);
}
console.log('词云布局测试通过');

// Test for the share-link logic in web/app.js.
//
// The address bar hash carries the template path and the parameters, so a
// preview link can be shared as is. The two functions are extracted and
// exercised here with Node.
//
//   node tools/tests/share_link_test.mjs
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const here = path.dirname(fileURLToPath(import.meta.url));
const src = fs.readFileSync(path.join(here, '..', '..', 'web', 'app.js'), 'utf-8');

const readHashSrc = src.match(/function readHash\(\) \{[\s\S]*?\n  \}/);
const syncHashSrc = src.match(/function syncHash\(\) \{[\s\S]*?\n  \}/);
if (!readHashSrc || !syncHashSrc) {
  console.error('未在 web/app.js 中找到 readHash / syncHash');
  process.exit(1);
}

// Replace the shared state with a controllable stand-in.
const syncFixed = syncHashSrc[0]
  .replace(/state\.path/g, 'ST.path')
  .replace(/state\.params/g, 'ST.params');

const loc = { hash: '' };
const history = {
  replaceState: (a, b, u) => {
    loc.hash = u.startsWith('#') ? u.slice(1) : u;
  }
};
const ST = {
  path: 'demo/basic/分组统计.cpt',
  params: { 区域: '华东', 空值: '' }
};

const api = new Function(
  'location', 'history', 'ST',
  readHashSrc[0] + '\n' + syncFixed + '\nreturn { readHash, syncHash };'
)(loc, history, ST);

let failures = 0;
function check(name, cond, detail) {
  if (cond) {
    console.log('PASS ' + name);
  } else {
    failures++;
    console.log('FAIL ' + name + (detail ? ' -> ' + detail : ''));
  }
}

// An empty hash means nothing to open.
check('空 hash 返回 null', api.readHash() === null);

// A hash with a path and a parameter must be decoded.
loc.hash = 'path=demo%2Fbasic%2F%E5%88%86%E7%BB%84%E7%BB%9F%E8%AE%A1.cpt' +
           '&%E5%8C%BA%E5%9F%9F=%E5%8D%8E%E4%B8%9C';
const parsed = api.readHash();
check('路径正确解码', parsed.path === 'demo/basic/分组统计.cpt', parsed && parsed.path);
check('参数正确解码', parsed.params['区域'] === '华东', JSON.stringify(parsed && parsed.params));

// Writing the hash back must produce a link that parses to the same thing,
// and empty parameters must be dropped.
loc.hash = '';
api.syncHash();
check('空参数被剔除', loc.hash.indexOf('空值') < 0, loc.hash);
const back = api.readHash();
check('往返路径一致', back.path === ST.path, back && back.path);
check('往返参数一致', back.params['区域'] === '华东', JSON.stringify(back && back.params));

if (failures > 0) {
  console.error(`分享链接测试失败：${failures} 项`);
  process.exit(1);
}
console.log('分享链接测试通过');

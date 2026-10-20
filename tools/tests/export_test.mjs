// Checks the export feature in web/app.js, web/index.html and web/style.css.
//
// The export is implemented in the browser, so the assertions here are about
// the pieces being present and wired together. Run it with:
//
//   node tools/tests/export_test.mjs
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.join(here, '..', '..', 'web');
const src = fs.readFileSync(path.join(webDir, 'app.js'), 'utf-8');
const html = fs.readFileSync(path.join(webDir, 'index.html'), 'utf-8');
const css = fs.readFileSync(path.join(webDir, 'style.css'), 'utf-8');

let failures = 0;
function check(name, cond, detail) {
  if (cond) {
    console.log('PASS ' + name);
  } else {
    failures++;
    console.log('FAIL ' + name + (detail ? ' -> ' + detail : ''));
  }
}

// The functions the export button relies on.
for (const fn of ['exportReport', 'exportPng', 'exportPdf', 'downloadDataUrl', 'exportFileName']) {
  check('存在函数 ' + fn, new RegExp('function\\s+' + fn + '\\s*\\(').test(src));
}

// The menu markup.
for (const attr of ['id="exportBtn"', 'id="exportMenu"', 'data-export="png"', 'data-export="pdf"']) {
  check('HTML 含 ' + attr, html.includes(attr));
}

// The button is wired to the menu.
check('导出按钮已绑定事件', /exportBtn[\s\S]{0,200}addEventListener/.test(src));
check('菜单项已绑定事件', /exportMenu[\s\S]{0,300}addEventListener/.test(src));

// The print stylesheet must hide the application chrome.
check('存在打印样式', css.includes('@media print'));
const printBlock = css.slice(css.indexOf('@media print'));
check('打印时隐藏侧栏', printBlock.includes('#sidebar') && printBlock.includes('display: none'));
check('打印时隐藏工具栏', printBlock.includes('.toolbar'));
check('打印时恢复原始尺寸', printBlock.includes('.page-scale') && printBlock.includes('transform: none'));

// The PNG export must combine every chart rather than only the last one.
check('PNG 导出遍历所有画布', /querySelectorAll\('canvas'\)/.test(src) &&
  /drawImage/.test(src));

if (failures > 0) {
  console.error(`导出功能检查失败：${failures} 项`);
  process.exit(1);
}
console.log('导出功能检查通过');

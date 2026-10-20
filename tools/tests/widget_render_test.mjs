// Front-end test for the fill-in control renderer in web/app.js.
//
// The renderer builds DOM nodes, so the test drives it with a minimal DOM
// stub and checks the element type, value and options it produces.
//
//   node tools/tests/widget_render_test.mjs
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const here = path.dirname(fileURLToPath(import.meta.url));
const appJs = fs.readFileSync(path.join(here, '..', '..', 'web', 'app.js'), 'utf-8');

const match = appJs.match(/function buildFormControl\(cell\) \{[\s\S]*?\n  \}\n/);
if (!match) {
  console.error('未在 web/app.js 中找到 buildFormControl');
  process.exit(1);
}
const matchNormalize = appJs.match(/function normalizeDate\(v\) \{[\s\S]*?\n  \}\n/);
if (!matchNormalize) {
  console.error('未在 web/app.js 中找到 normalizeDate');
  process.exit(1);
}
const matchPad2 = appJs.match(/function pad2\(n\) \{[\s\S]*?\n  \}\n/);
if (!matchPad2) {
  console.error('未在 web/app.js 中找到 pad2');
  process.exit(1);
}

// ---- minimal DOM stub ----
class El {
  constructor(tag) {
    this.tagName = tag;
    this.children = [];
    this.className = '';
    this.style = {};
    this.attrs = {};
    this.textContent = '';
    this.value = '';
    this.checked = false;
    this.disabled = false;
    this.selected = false;
    this.type = '';
    this.title = '';
    this.listeners = {};
  }
  appendChild(c) {
    this.children.push(c);
    return c;
  }
  addEventListener(name, fn) {
    (this.listeners[name] = this.listeners[name] || []).push(fn);
  }
  setAttribute(k, v) {
    this.attrs[k] = v;
  }
  get first() {
    return this.children[0];
  }
}
global.document = {
  createElement: (tag) => new El(tag),
  createDocumentFragment: () => new El('#fragment'),
};

const buildWidget = new Function(
  'document',
  match[0] + matchPad2[0] + matchNormalize[0] +
    '; return { buildFormControl: buildFormControl, normalizeDate: normalizeDate };'
)(global.document);

let failed = 0;
function check(name, cond, detail) {
  if (cond) {
    console.log('PASS ' + name);
  } else {
    failed++;
    console.log('FAIL ' + name + (detail ? '  -> ' + detail : ''));
  }
}

// ---- number control ----
let w = buildWidget.buildFormControl({
  text: '1234.5',
  widget: { kind: 'number', editable: true },
});
check('数字控件生成 input', w.first.tagName === 'input', w.first.tagName);
check('数字控件类型为 number', w.first.type === 'number', w.first.type);
check('数字控件值为 1234.5', w.first.value === '1234.5', w.first.value);

// ---- text control strips the report's formatting ----
w = buildWidget.buildFormControl({
  text: '\n摘要',
  widget: { kind: 'text', editable: true },
});
check('文本控件保留内容', w.first.value === '\n摘要', JSON.stringify(w.first.value));

// ---- combo control keeps the current value and the options ----
w = buildWidget.buildFormControl({
  text: '宣传部',
  widget: { kind: 'combo', editable: true, options: ['联动部', '宣传部', '策划部'] },
});
check('下拉控件生成 select', w.first.tagName === 'select', w.first.tagName);
check('下拉控件选项数正确(含空项)', w.first.children.length === 4, String(w.first.children.length));
const selected = w.first.children.filter((o) => o.selected);
check('下拉控件选中当前值', selected.length === 1 && selected[0].value === '宣传部',
  JSON.stringify(selected.map((o) => o.value)));

// ---- combo value that is not among the options stays visible ----
w = buildWidget.buildFormControl({
  text: '自定义',
  widget: { kind: 'combo', editable: true, options: ['A', 'B'] },
});
const sel2 = w.first.children.filter((o) => o.selected);
check('下拉控件保留不在选项内的值',
  sel2.length === 1 && sel2[0].value === '自定义',
  JSON.stringify(sel2.map((o) => o.value)));

// ---- read-only control ----
w = buildWidget.buildFormControl({
  text: '只读值',
  widget: { kind: 'text', editable: false },
});
check('只读控件用 div', w.first.tagName === 'div', w.first.tagName);
check('只读控件显示值', w.first.textContent === '只读值', w.first.textContent);

// ---- button control ----
w = buildWidget.buildFormControl({
  text: '删除',
  widget: { kind: 'button', editable: false },
});
check('按钮控件生成 button', w.first.tagName === 'button', w.first.tagName);
check('按钮控件禁用', w.first.disabled === true, String(w.first.disabled));

// ---- checkbox ----
w = buildWidget.buildFormControl({
  text: 'true',
  widget: { kind: 'checkbox', editable: true },
});
check('复选框类型正确', w.first.type === 'checkbox', w.first.type);
check('复选框按值勾选', w.first.checked === true, String(w.first.checked));

// ---- date control normalises the report's date text ----
w = buildWidget.buildFormControl({
  text: '2010/5/1',
  widget: { kind: 'date', editable: true },
});
check('日期控件类型为 date', w.first.type === 'date', w.first.type);
check('日期控件值已规范化', w.first.value === '2010-05-01', w.first.value);

check('日期解析支持中文', buildWidget.normalizeDate('2010年5月1日') === '2010-05-01',
  buildWidget.normalizeDate('2010年5月1日'));
check('无法识别时返回空', buildWidget.normalizeDate('不是日期') === '',
  buildWidget.normalizeDate('不是日期'));

// ---- percentage keeps only the numeric part ----
w = buildWidget.buildFormControl({
  text: '12.5%',
  widget: { kind: 'percent', editable: true },
});
check('百分比控件去掉百分号', w.first.value === '12.5', w.first.value);

if (failed > 0) {
  console.error('\n' + failed + ' 项失败');
  process.exit(1);
}
console.log('\n填报控件渲染测试通过');

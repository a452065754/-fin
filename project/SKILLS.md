# 技能沉淀库

> **只追加。** 记录**可复用的方法、命令、模板**。
> 与 LESSONS 的区别：LESSONS 是「踩过的坑」，SKILLS 是「下次直接抄的」。

---

## 目录

| 分类 | 内容 |
|---|---|
| [S1. 调研摸底](#s1-调研摸底) | 摸格式、统计覆盖面、找样本模板 |
| [S2. 编码模板](#s2-编码模板) | 加图表类型的四步、解析新定义、连接构造 |
| [S3. 测试模板](#s3-测试模板) | 找样本、断言写法、真实模板验证 |
| [S4. 排查工具](#s4-排查工具) | trace 定位、统计命令、比对命令 |
| [S5. 交付流程](#s5-交付流程) | 提交前检查、文档更新、推送 |

---

## S1. 调研摸底

### S1.1 · 统计某种 XML 标签的类名分布

摸一个未知格式，先统计覆盖面，别急着写解析。

```bash
cd /f/tomcat-win64/webapps/webroot/WEB-INF/reportlets
grep -rhoP '<Plot class="[^"]*"' --include="*.cpt" . \
  | sed 's/.*class="//;s/"//' | sort | uniq -c | sort -rn
```

输出直接告诉你哪几类值得实现、哪几类可以忽略（频次 1–2 的不急）。

### S1.2 · 统计父节点下的子节点种类

```bash
grep -rhoP '<ChartDefinition>\s*<(\w+)' --include="*.cpt" . \
  | sed 's/.*<//' | sort | uniq -c | sort -rn
```

**这招的价值**：本项目就靠它发现「真正的瓶颈是 12 种数据定义，不是图表
类型映射」，避免了走错方向。

### S1.3 · 找一个结构的具体样本

```bash
# 按类名找含该结构的模板
grep -rl "VanChartBubblePlot" --include="*.cpt" . | head -1
```

拿它当测试样本 + 人工观察结构的依据。

### S1.4 · 看某结构在 XML 里的完整上下文

```bash
python3 -c "
import re
d=open(r'文件.cpt',encoding='utf-8',errors='replace').read()
i=d.find('关键词')
print(d[max(0,i-300):i+1500])
"
```

`errors='replace'` 很关键 —— 模板里可能有非法编码字节，不加会抛异常。

### S1.5 · 批量提取同结构样本

```python
import re, glob
for f in glob.glob('**/*.cpt', recursive=True):
    d = open(f, encoding='utf-8', errors='replace').read()
    m = re.search(r'<目标标签.*?</目标标签>', d, re.S)
    if m:
        print(f); print(m.group(0)[:800]); break
```

---

## S2. 编码模板

### S2.1 · 加一个新图表类型：四步

改图表类型**必须同时改四处**，漏一处就白做：

| 步 | 文件 | 改什么 |
|---|---|---|
| 1 | `internal/fr/chart.go` | 类型分发表（`switch` on Plot class）→ 决定 `spec.Kind` |
| 2 | `internal/fr/chart.go` | 定义分发表（`switch` on definition name）→ 决定走哪个解析器 |
| 3 | `internal/fr/chart.go` | `decorateSeries` → 补该类型特有的系列字段 |
| 4 | `web/app.js` | `chartOption` → 加渲染分支 |

**顺序不能反**：第 1 步决定第 2 步的分支会不会走到。

### S2.2 · 挑定义节点的正确写法

```go
var inner *XNode
var wrapper *XNode
for _, n := range def.Children {
    lname := strings.ToLower(n.Name)
    if !strings.Contains(n.Name, "Definition") { continue }
    // 显式跳过包装层
    if lname == "datadefinition" || lname == "areadefinition" ||
       lname == "linkdefinition" {
        if wrapper == nil { wrapper = n }
        continue
    }
    // 按「是否自带数据」择优，而不是按名字
    if n.Child("TableData") != nil || n.Child("GanttAttr") != nil {
        inner = n
        break
    }
    if inner == nil { inner = n }
}
if inner == nil { inner = wrapper }
```

配套：`datasetOf` 要**递归**查，别只查直接子节点。

### S2.3 · 递归找数据集

```go
func datasetOf(n *XNode, datasets map[string]*Dataset) *Dataset {
    if n == nil { return nil }
    if td := n.Child("TableData"); td != nil {
        if name := td.Child("Name"); name != nil {
            return datasets[name.TextTrim()]
        }
    }
    if name := n.Child("Name"); name != nil {
        return datasets[name.TextTrim()]
    }
    for _, c := range n.Children {          // ← 递归这一层是关键
        if ds := datasetOf(c, datasets); ds != nil { return ds }
    }
    return nil
}
```

### S2.4 · 日期值转数值（不改公共函数）

```go
// 图表层专用包装，不动 toFloat
func toFloatOrDate(v any) (float64, bool) {
    if f, ok := toFloat(v); ok { return f, true }
    s, ok := v.(string)
    if !ok { return 0, false }
    if ms, ok := parseDate(s); ok { return float64(ms) / 86400000, true }
    return 0, false
}
```

日期 layout 按出现频率排：

```go
var dateLayouts = []string{
    "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04",
    "2006-01-02", "2006/01/02 15:04:05", "2006/01/02", "2006年01月02日",
}
```

### S2.5 · 构造 DSN（4 类数据库）

| 类型 | DSN 形式 | 注意 |
|---|---|---|
| sqlite | `file:<path>?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(3000)` | `mode=ro` + `query_only` 双保险只读 |
| mysql | `<user>:<pwd>@tcp(<host>:<port>)/<db>?parseTime=false&timeout=10s` | `parseTime=false` 避免时间类型转换 |
| oracle | `oracle://<user>:<pwd>@<host>%3A<port>%3A<sid>` | **冒号/斜杠必须 URL 转义** |
| sqlserver | `url.Values` 编码 | **类型必须是 `url.Values`**，写 `map[string][]string` 没有 `Set`/`Encode` |

### S2.6 · 中文名转环境变量名

```go
func envSafe(name string) string {
    // 纯 ASCII 直接用名；含中文用 UTF-8 字节的十六进制
    var hex strings.Builder
    for _, c := range []byte(name) {
        fmt.Fprintf(&hex, "%02X", c)
    }
    return hex.String()
}
```

必须**校验不同名字不撞车**（见 `connection_test.go` 的
`TestPasswordEnvFor`）。

---

## S3. 测试模板

### S3.1 · 断言要同时覆盖类型和数据量

```go
func firstChartOfKind(t *testing.T, rel, kind string) *ChartSpec {
    t.Helper()
    charts := chartsIn(t, rel)
    for _, c := range charts {
        if c.Kind == kind { return c }
    }
    t.Logf("%s 中的图表类型: %s", rel, kindsOf(charts))
    t.Fatalf("%s 中未找到 %s 图表", rel, kind)
    return nil
}

// 关键：数据量也要断言
if len(c.Series) == 0 { t.Fatal("甘特图没有数据系列") }
total := 0
for _, s := range c.Series { total += len(s.Data) }
if total == 0 { t.Fatal("甘特图系列没有数据点") }
```

### S3.2 · 数据为空是合法情况时

```go
if len(ds.Rows) == 0 {
    t.Log("内置数据为空（Ascii85/Deflate 解码截断），本次只验证类型识别")
}
```

断言改验**解析行为**（类型对、告警不误报），不验数据量。

### S3.3 · 起服务做端到端验证

```bash
export GOCACHE=/f/QDAKE/.gocache
bash tools/build_windows.sh bin/reportviewer.exe
./bin/reportviewer.exe -addr 127.0.0.1:8099 &

curl -s --noproxy '*' "http://127.0.0.1:8099/api/preview?path=xxx.cpt" \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)
for sh in d.get('sheets',[]):
    for c in sh.get('cells',[]):
        if c.get('chart'):
            ch=c['chart']
            print('kind=',ch['kind'],'series=',len(ch.get('series',[])))
"
```

**单元测试过了不等于端到端对了** —— 数据要真的从磁盘模板跑到 API 输出。

### S3.4 · 环境不存在时 skip 而非 fail

```go
cs, err := LoadConnections(testWebroot)
if err != nil {
    t.Skipf("webroot 不可用，跳过: %v", err)
}
```

但要注意：**skip 的测试等于没测**，覆盖率要盯着看。

---

## S4. 排查工具

### S4.1 · 加临时 trace 定位 XML 层级

定位不到节点时最有效的一招：

```go
fmt.Printf("[trace] inner=<%s> children=%d\n", inner.Name, len(inner.Children))
for _, n := range def.Children {
    fmt.Printf("[trace]   child <%s> td=%v gantt=%v\n",
        n.Name, n.Child("TableData") != nil, n.Child("GanttAttr") != nil)
}
```

**定位完删掉**：`grep -rn "trace" internal/` 应无输出。

### S4.2 · 本轮实战：靠 trace 抓到的三个 bug

| bug | trace 揭示了什么 |
|---|---|
| heatmap 被当地图 | 分支顺序问题，看代码即可发现 |
| 定义节点挑错 | `selected inner=<VanGanttDefinition> children=2` → 发现少一层 |
| 甘特图数据为空 | 值全是 string `2015-01-06 00:00:00` → `toFloat` 失败 |

**结论**：**猜不出来就加 trace**。本轮有两次凭直觉改代码都改错了，
加 trace 一次就对。

### S4.3 · 双向差集比对（校验文档与代码）

```bash
grep -oP 'spec\.Kind = "\K[a-z0-9]+' internal/fr/chart.go | sort -u > /tmp/k1.txt
# 从文档里提取同名清单写进 /tmp/k2.txt
comm -23 /tmp/k1.txt /tmp/k2.txt   # 代码有文档无
comm -13 /tmp/k1.txt /tmp/k2.txt   # 文档有代码无
```

### S4.4 · 统计代码规模

```bash
find . -name "*.go" -not -path "./tmpcheck/*" -exec wc -l {} + | tail -1
```

> **坑**：`find ... | xargs wc -l` 在路径含中文/空格时会报
> `assertion "bc_ctl.arg_max >= LINE_MAX" failed`。用 `-exec ... +` 代替管道。

### S4.5 · 用 Java 原版做基准对照（反汇编 + 直接调用）

**场景**：解析结果和某个第三方产品对不上，猜参数猜不出来。

```bash
# 1. 找到对方的 class（FineReport 的在 WEB-INF/lib 里）
cd /tmp && mkdir oracle && cd oracle
unzip -o -q /f/tomcat-win64/webapps/webroot/WEB-INF/lib/fine-cbb-11.0.jar \
    "com/fr/stable/Ascii85*"

# 2. 反汇编看行为
javap -c -p com/fr/stable/Ascii85InputStream.class

# 3. 直接当库调用，输出 hex 作为 ground truth
#    （注意：class 要放在 com/fr/stable/ 目录下，javac -cp . -d .）
java -cp . Ascii85Oracle < payloads.txt > oracle.txt

# 4. 与自己的实现逐条比对
```

**项目内的基准工具**：`tools/oracle/Ascii85Oracle.java`

**要点**：

- Java 源码用 **纯 ASCII**（本机 javac 默认 GBK，中文注释会报
  `错误: 编码GBK的不可映射字符`），加 `-encoding UTF-8` 编译
- 用 `Inflater` 直接喂数据时必须自己调 `setInput`，用
  `InflaterInputStream` 更省事
- 逐条比对脚本：读两行文件（载荷 / 期望 hex），比 hex
- **结论**：本项目 718/718 条载荷与 Java 原版字节级一致

---

## S5. 交付流程

### S5.1 · 提交前检查（一条命令）

```bash
export GOCACHE=/f/QDAKE/.gocache
gofmt -l . 2>/dev/null | grep -v tmpcheck   # 应无输出
go vet ./internal/... ./cmd/...              # 应无输出
go test ./...                                # 应全绿
node --check web/app.js                      # 前端语法
python3 tools/verify_docs.py                 # 文档自检
```

### S5.2 · 文档自检工具用的是什么

`tools/verify_docs.py` 做 7 类检查：

| 检查 | 方法 |
|---|---|
| 相对链接有效性 | 提取所有 `](...)` 逐个 stat |
| 源码文件引用 | 提取 `` `internal/...` `` 形式的引用逐个 stat |
| 状态标记合法性 | ROADMAP 表格里的标记须在白名单内 |
| 关键数字一致 | 测试数、Kind 清单、API 端点、flag 与代码比对 |
| 只追加完整性 | 用 `git show HEAD:file` 比对行数，被截短则报错 |
| 进度与提交一致 | PROGRESS 阶段数 vs git 提交数 |
| 索引完整性 | INDEX 必须链到全部知识库文件 |

**验证工具有效性的方法**（重要）：写完自检工具后**必须注入故障**验证它
真能抓到问题，否则「PASS」毫无意义：

```bash
# 注入断链
sed -i 's|\[STATUS.md\](STATUS.md)|[STATUS.md](不存在.md)|' project/INDEX.md
# 注入少写 Kind
sed -i 's|`bubble` / ||' docs/使用.md
python3 tools/verify_docs.py     # 应 FAIL 并指出具体问题
# 恢复
git checkout project/INDEX.md docs/使用.md
```

### S5.3 · 每个功能完成后的文档更新（6 步）

```
1. 更新 project/STATUS.md      快照：把功能加进「已支持」
2. 更新 project/ROADMAP.md     勾掉任务，状态 ✅，转遗留项
3. 追加 project/PROGRESS.md    做了什么 / 怎么验证的 / 提交号
4. 追加 project/LESSONS.md     踩的坑（没有就跳过）
5. 追加 project/SKILLS.md      可复用方法（没有就跳过）
6. 提交并推送
```

**不要攒到最后一次性更新** —— 攒着就会忘，忘了文档就会失真。

### S5.4 · 提交信息模板

```
<type>: <一句话做了什么>

<为什么这么做 / 放弃了什么方案>

<关键实现点，按条目列>

<顺手修了哪些 bug>
```

`type` 用：`feat` / `fix` / `docs` / `refactor` / `test` / `chore`

### S5.5 · 推送后确认

```bash
git push origin main 2>&1 | grep -v credential-manager | tail -3
git ls-remote origin        # 确认远程 commit 号与本地一致
```

**不要被 `credential-manager: No such file` 那行干扰**（见 LESSONS C5）。

---

### S6 · 用「真实产品模板全量扫描」量化功能覆盖率

**适用**：任何需要兼容第三方格式/协议的项目。要回答「我的实现覆盖了多少」
这类问题时，不要估计，直接扫。

**做法**（本项目实例）：

1. **找到权威样本**：本项目是 FineReport 安装目录下的 1550 个官方模板
2. **写出提取规则**：正则抽出每个单元格的公式文本
   （`class="com.fr.base.Formula"` → `<Attributes>` 里的 CDATA）
3. **统计频次排优先级**：`SUM` 1782 次 → 先做；`SQL` 92 次 → 后做
4. **实际渲染 + 归因**：不只看成功/失败，要把失败**分类**
   （代码可修 / 渲染范围缺口 / 环境限制）
5. **把工具留在仓库里**：`tools/formula_scan`，改完立刻跑，看数字变化

**为什么必须归因**：只报「还差 1164 个」没法下手。归因后发现：

| 类别 | 占比 | 该不该修 |
|---|---|---|
| 语法未覆盖 | 26.5% | 修 |
| 上游失败连带 | 23.5% | 修上游 |
| 渲染范围缺口 | 16.6% | 修渲染器 |
| 环境限制（模板文件不存在） | 16.1% | **别修，写进文档** |

**关键验证**：判断「环境限制」要拿**数量交叉验证** ——
234 个公式直接调 `value("报表名",..)`，被引用的 10 个报表名在磁盘上
一个都不存在，234 直接引用 → 231 次失败，数量对上了才能下结论。

**产出**：一份可量化的报告（模板数 / 公式单元格数 / 失败数 / 归因分布 /
函数频次），比「支持了 XX%」这种没有依据的说法有用得多。

---

---

### S7 · 判断「是否支持某能力」：在官方样本里找标志，而不是在代码里找

**适用**：任何「兼容某第三方格式/产品」的项目。用户问「X 支持吗」时。

**做法**（本项目实例）：

1. **找到权威样本**：FineReport 安装目录下的 1550 个官方模板
2. **在样本里找标志**（而不是在项目代码里找）：
   ```bash
   # H5 模板的标志
   grep -l "ElementCaseMobileAttr" -r reportlets | wc -l
   # 填报控件的标志
   grep -o '<Widget class="[^"]*"' -r reportlets | sort | uniq -c
   ```
3. **判断是「结构差异」还是「数据差异」**：
   - H5 模板 = 普通 CPT + 一个额外节点 → **不用新解析器**
   - 填报控件 = 普通单元格 + 一个兄弟节点，值仍在 `<O>` → **不用改数据流**
4. **量化规模再动手**：16 万个控件单元格、250 个 H5 模板 →
   值得做，且知道覆盖了多少

**关键洞察**：很多「不支持」其实是「没识别到标志」。
**先找到标志，往往发现只是加一个分支**。

---

### S8 · 三层验证：后端单测 → 接口 curl → 浏览器截图

**适用**：任何带前端的项目。

| 层 | 能发现什么 | 发现不了什么 |
|---|---|---|
| Go 单测 | 后端逻辑错误 | 前端任何问题 |
| Node 单测（抽函数） | 单个函数的逻辑 | **整个脚本跑不起来**、函数重名 |
| curl 接口 | JSON 契约、HTTP 状态 | 前端渲染、布局、交互 |
| **浏览器截图** | 整页崩了、布局错、样式没生效 | 后端数据对不对 |

**本项目实例**：函数重名导致 FVS 大屏渲染崩溃，三层检查全绿，
**只有截图能发现**（页面写着「模板渲染失败」）。

**截图注意事项**：

- 要**真的看图**，不能只看文件大小 —— 报错页和正常页大小差不多
- 本机 Chrome headless 要加 `--no-proxy-server`，
  否则公司代理拦本地请求（`ERR_CONNECTION_REFUSED`）
- 改前端后要加 `?v=` 版本号，否则浏览器缓存旧 JS，
  **改了等于没改**

**建议**：把「截一张关键页面的图」写进提交前检查清单。

---

---

### S9 · 受限能力做成「开关」，而不是「不做」

**适用**：任何「按用户要求打开某个危险能力」的场景。

**做法**（本项目实例）：

1. **建一个策略结构**（`internal/fr/policy.go`），把每个受限能力变成一个字段，
   **默认值全关**
2. **四层配置**，从低到高：内置默认 → `config.json` → 环境变量 → 命令行
3. **在语法层拦截**，不是在执行层 —— 模板作者改公式时立刻看到原因
4. **错误信息写明开关名**：
   ```
   受限能力 SQL() 公式函数 已被安全防护开关关闭（如需开启请设置 RV_ALLOW_FORMULA_SQL=1）
   ```
5. **状态可观测**：`GET /api/health` 的 `policy` 字段返回当前生效设置

**为什么这样做比「直接实现」好**：

- 用户说「内网使用，防护先不考虑」——那是**部署时的选择**，代码不该写死
- 交付给别人的东西，别人机器上也可能要关
- 开关在文档里写清楚后，运维能自己决定

**关键**：把「要不要开」变成**一次显式的动作**，而不是代码里的隐含假设。

---

### S10 · 改动语义后，用「全量扫描」验证方向对不对

**适用**：任何会改变求值语义的改动。

**做法**：改之前先记下基线数字（`go run ./tools/formula_scan`），
改完再跑一次对比。

**本项目实例**：实现单元格切片时，因为猜错了「层次坐标」的语义，
总失败数从 **1130 涨到 4336** —— 一眼就知道方向错了。

**二分定位法**：把新特性临时改成「直接返回错误」，如果总数回到基线，
说明问题在新特性内部，而不是别处被波及。

**另一个坑**：改一个符号的含义，要同时验**正例和反例**。
`&` 从「拼接」改成「层次引用」时，`"a"&B3` 被误判 —— 靠全量扫描才发现。

---

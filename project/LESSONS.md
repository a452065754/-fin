# 经验教训库

> **只追加，不删改。** 记录踩过的坑与得出的结论。
> 每条包含：现象 → 根因 → 结论。**只写结论没用，要写「怎么发现的」**，
> 否则下次遇到同样的现象还是不知道从哪查起。

---

## 目录

| 分类 | 条目 |
|---|---|
| [A. 解析与数据](#a-解析与数据) | A1–A8 |
| [B. 测试](#b-测试) | B1–B3 |
| [C. 工具链与环境](#c-工具链与环境) | C1–C5 |
| [D. 流程与协作](#d-流程与协作) | D1–D5 |

---

## A. 解析与数据

### A1 · `strings.Contains` 做类型判断必须按特异性排序

**现象**：热力图（heatmap）被渲染成地图。

**根因**：图表类型分发表里，地图分支是 `strings.Contains(cls, "map")`，
而 `"heatmap"` 里**包含子串 `map`**。map 分支在前，heat 就被截胡。

**结论**：用子串匹配做类型分派时，**必须按特异性从高到低排**。有重叠
风险时优先用更精确的匹配（`HasSuffix`、完整类名比较、或先剥离已知后缀）。

**同类风险**：`multipie` vs `pie`、`bubble` vs `pie`、`drillmap` vs `map`。

**怎么发现的**：测试断言 `kind == "heatmap"` 失败，顺藤摸瓜查到分发表顺序。

---

### A2 · `Find` 递归整棵树，可能不是你想要的那一层

**现象**：甘特图、组合图都取不到数据，但节点明明存在。

**根因**：`XNode.Find(name)` 递归遍历**整棵子树**返回第一个匹配。
XML 里 `<ChartDefinition><VanGanttDefinition><dataDefinition
class="...VanGanttTableDefinition">` 这种包装结构，匹配到的节点跟预期
差了两层。

**结论**：

- 已知层级用 `Child`（只看直接子节点），层级不确定才用 `Find`
- `Find` 命中后**要确认是哪一层**。拿不准就加临时 trace 打 print：

```go
fmt.Printf("[trace] inner=<%s> children=%d\n", inner.Name, len(inner.Children))
for _, n := range def.Children {
    fmt.Printf("[trace]   child <%s> td=%v\n", n.Name, n.Child("TableData") != nil)
}
```

**定位完务必删掉**（提交前 `grep -rn "trace"` 检查）。

---

### A3 · 挑「带某种属性的子节点」时，会被包装节点截胡

**现象**：明明有 `TableData` 的定义节点，却挑中了空的包装节点。

**根因**：用 `strings.Contains(n.Name, "Definition")` 挑第一个子节点，
结果选中 `<dataDefinition class="...TableDefinition">` 这类**只做包装、
自己不带数据**的节点。

**结论**：挑节点时不要按**名字**挑，要按**内容特征**挑：

```go
// ✓ 按「是否自带数据引用」择优
if n.Child("TableData") != nil || n.Child("GanttAttr") != nil {
    inner = n
    break
}
```

同时**显式跳过已知包装层**：`datadefinition` / `areadefinition` /
`linkdefinition`。

---

### A4 · HSQLDB 的 `db.script`：分隔符是 3 个字符

**现象**：解析出来所有值都带一个前导单引号，导致 14 个连接一个都认不出。

**根因**：INSERT 语句是 `VALUES('key','value')`，key 与 value 之间的分隔符
`','` 是**引号-逗号-引号共 3 个字符**。代码里按 2 跳过
（`rest[q+2:]`），把分隔符的第三个字符（引号）当成了值的开头。

**结论**：字符串字面量分隔符的长度要数清楚。这类"差一个字符"的 bug
**症状是全体数据都带同一个前缀**，一眼能看出是解析问题，不是数据问题。

**另外两个坑**（同文件）：

- 中文连接名在 key 里是 `\uXXXX` 转义
  （`ConnectionConfig.connections.\u6c14\u5bc6\u68c0\u6d4b77.url`），
  必须先还原成真实字符才能按名字匹配
- SQL 字面量内的 `''` 是转义单引号

---

### A5 · 改 `toFloat` 会波及公式层

**现象**：甘特图所有行都被跳过，提示"没有有效的任务数据"。

**根因**：模板里的日期列值是**字符串**（`2015-01-06 00:00:00`），
`toFloat` 转换失败。但 `toFloat` 被公式求值器共用，直接改它的语义会
影响整个公式层。

**结论**：**不要动被多处共用的基础函数的语义**。在需要的地方加包装函数：

```go
func toFloatOrDate(v any) (float64, bool) {
    if f, ok := toFloat(v); ok { return f, true }
    // 尝试 7 种日期 layout
}
```

---

### A6 · 内置数据解码失败，可能是"加密"而非"截断"

**现象**：FVS/CPT 模板的内置数据只有约 36% 能解压成功（718 段载荷中 454 段失败）。

**根因（2026-10-20 已查明大半）**：**不是解码器的问题**。用 FineReport
自带的 `com.fr.stable.Ascii85InputStream`（在 `WEB-INF/lib/fine-cbb-11.0.jar`）
做基准对照，**718/718 条载荷的解码结果与我们的 Go 实现字节级完全一致**。
Java 版对同一载荷做 `Inflater` 解压时同样抛
`ZipException: invalid distance code`。

也就是说：**这些模板文件里的载荷，FineReport 自己也解不出来**。剩余失败
的部分要么是文件本身的数据缺陷，要么存在我们尚未发现的前置处理步骤
（加密/混淆），**不是 Ascii85 语义理解错了**。

**已排除的假设**（都实测过）：

| 假设 | 实测结果 |
|---|---|
| Ascii85 末组补位字符不对（`!` vs `u`） | 两种都试，成功率都 27% |
| 末组是否丢弃最后一个字节 | 丢弃/不丢弃都试，差异 ≤ 1% |
| Deflate 窗口参数不对 | wbits 从 -15 到 15 全试，最佳仍是 raw deflate |
| 载荷按行独立编码 | 29%，比跳空白更差 |
| 载荷里有换行导致分组错位 | 跳空白能提升 27%→40%，但仍是同一批失败 |
| 标准 `base64.a85decode` | 0% |

**当前策略**：保留可解码前缀降级使用，并在告警里说明「已使用可解码部分」。

**方法论沉淀（比结论更重要）**：**遇到"和某个产品对不上"的问题，第一反应
应该是去找对方的原始实现做基准对照**，而不是继续猜参数。FineReport 的
class 文件就在 `WEB-INF/lib` 里，`javap -c` 反汇编 + 直接跑 Java 就能拿到
ground truth。基准工具保存在 `tools/oracle/Ascii85Oracle.java`。

**遗留**：载荷在 Ascii85 之前是否还有一步处理（如异或/替换）尚未查清，
需要 FineReport 写出 RowData 的那一层代码。

**附带经验**：项目里**已有** `decodeAscii85Java` 复刻了 Java 版行为，
新做解码前先看它，别重复造。

### A7 · 数据集自己的参数默认值，之前根本没生效

**现象**：做「引用单元格的图表」时，图表取不到数。追下去发现根因不在图表，
而在**数据集加载**。

**根因**：模板的每个数据集可以自带参数与默认值：

```xml
<TableData name="ds1" class="com.fr.data.impl.DBTableData">
  <Parameters>
    <Parameter>
      <Attributes name="地区"/>
      <O><![CDATA[华东]]></O>
    </Parameter>
  </Parameters>
  <Query><![CDATA[SELECT * FROM [销量]A where 地区 ='${地区}']]></Query>
</TableData>
```

`Dataset.Load` 只把**请求参数**传进 `ExpandSQL`，完全忽略了数据集自己的
默认值。请求没给「地区」时，`${地区}` 展开成空串 → SQL 变成
`where 地区 =''` → 查不到行 → 数据集 `loaded=false` → 报告里所有单元格
都是空的 → 引用单元格的图表自然也拿不到值。

**影响面**：**所有带数据集参数的模板**，不止图表。这是长期存在的 bug。

**结论**：

1. 参数解析要分两层：**模板级**（`ReportParameterAttr`，已实现）和
   **数据集级**（`<TableData><Parameters>`，本次补上）
2. 合并规则：**请求参数 > 数据集默认值 > 空**
3. `Dataset` 结构体加 `Params []ParamDef`，`effectiveParams` 做合并

**怎么发现的**：不是一次想通的，是「图表取不到数 → 查单元格值 → 单元格
也是空 → 查数据集 → `loaded=false` → 手工跑 SQL 有 25 行 → 对比发现
只有走 `Load` 才有问题」这样逐层剥出来的。**遇到「数据不对」要从
最底层往上查，别在表层猜。**

### A8 · 词云布局：不要用螺旋，用行填充

**背景**：词云要渲染，但 `echarts-wordcloud` 插件要求 ECharts ^5.0.1，
而项目里打包的是 **ECharts 4.1.0**。升级 ECharts 会影响全部图表（回归面
太大），于是决定自研布局。

**试过的方案与结果**：

| 方案 | 放置率 | 问题 |
|---|---|---|
| 单条阿基米德螺旋 | 15/40 | 路径浪费空间，画布只用掉 18% |
| 5 臂螺旋 | 21/40 | 仍填不满 |
| **行填充（最终采用）** | **40/40** | 无 |

**结论**：词云要的是**填满区域**，不是画一条漂亮的曲线。行填充（大词优先、
逐行摆放、行内按字号排序、每行居中）实现简单且效果稳定。

**三个实现坑**：

1. **行内 x 坐标要累加**。最初写成 `x: offset + it.w/2` 忘了 `x += it.w`，
   结果同一行的词全挤在一起（dx=0.4），**目视测试没发现，是用脚本查重叠
   才暴露的**
2. **间距不能同时算进两个词的宽度**。gap 要么加在词的 box 上、要么单独
   加，否则相邻两词的距离会差一个 gap
3. **字号必须夹紧**。权重区间退化时 ratio 可能算出负数或 NaN，字号变负 →
   词跑到画布外。加 `Math.max(10, Math.min(48, ...))` 兜底

**怎么验证的**：`tools/tests/wordcloud_layout_test.mjs` —— 把 `layoutWordCloud`
从 app.js 里抽出来用 Node 跑，断言「全部放置 / 零重叠 / 零越界 / 字号合法」。
10、40、80 词三档全过。

**通用经验**：**布局算法必须写自动化测试**。目视看截图只能发现「明显不对」，
而「词全叠在一起」这种问题只有量化断言才抓得住。

### B3 · 测试必须能在「没有 FineReport」的机器上跑

**现象**：写 GitHub Actions 时才发现，CI 机器上**没有 FineReport**，
而大部分测试都要读 webroot 里的模板 → 直接 **FAIL**（不是 skip），
CI 根本跑不起来。

**根因**：测试里 `webroot` 是一个写死的常量，找不到文件时
`ParseCPTFile` 报错 → `t.Fatalf` → 测试失败。

**结论**：

1. `webroot` 改成**可用环境变量覆盖**的变量：
   `FR_TEST_WEBROOT`，默认仍是本机路径
2. 加一个 `requireWebroot(t)` 守卫：模板目录不存在就 `t.Skip`
3. 给所有用到 webroot 的测试函数批量插入守卫（16 处）

**验证**：

```
FR_TEST_WEBROOT=F:/nonexistent go test ./...
  → ok，16 个跳过、14 个仍跑、退出码 0
本机 go test ./...
  → 61 个全过、0 个跳过
```

**通用经验**：**CI 环境与本地环境不同，测试要能在「依赖缺失」时优雅降级，
而不是崩掉**。写 CI 之前先模拟一次「依赖不在」的机器。

---

## B. 测试

### B1 · 只断言类型会假通过 —— 必须同时断言数据量

**现象**：新写的 chart 测试全绿，但组合图/甘特图/层级图的数据其实
一条都没取到。

**根因**：断言只查了 `kind == "gantt"`，`len(series) == 0` 也照样 PASS。
白排查了一轮。

**结论**：**新测试必须同时断言类型 + 数据量**：

```go
// ✗ 假通过
if c.Kind != "gantt" { t.Error("类型不对") }

// ✓ 必须确认真的取到数据
if len(c.Series) == 0 { t.Fatal("甘特图没有数据系列") }
total := 0
for _, s := range c.Series { total += len(s.Data) }
if total == 0 { t.Fatal("甘特图系列没有数据点") }
```

收紧断言后立刻暴露 3 个 FAIL，那才是真实状态。

**推广**：任何"解析类"测试都要问一句 —— **如果解析出来是空的，这个测试
还能过吗？** 能过就说明断言不够。

---

### B2 · 数据本身为空是合法情况时，要显式说明而不是让测试失败

**现象**：层级图模板测试失败，但查下来是**模板的内置数据本身解不出来**
（Ascii85 截断），不是代码 bug。

**结论**：区分「代码错了」和「数据本来就没有」。后者要写成：

```go
if len(ds.Rows) == 0 {
    t.Log("内置数据为空（Ascii85/Deflate 解码截断），本次只验证类型识别")
}
```

并把断言改成验证**解析行为**（类型识别正确、告警不误报），而不是数据量。

---

## C. 工具链与环境

### C1 · Go build cache 默认目录不可写

**现象**：`go build` / `go test` 报
`open ...\go-build\49\...-d: Access is denied`。

**解决**：指定可写目录

```bash
export GOCACHE=$(pwd)/.gocache
```

**为什么重要**：不解决这个，**所有 Go 命令都跑不起来**，看起来像代码
坏了，其实是环境问题。

---

### C2 · 依赖版本必须锁在 Go 版本兼容线

**现象**：`go get github.com/go-sql-driver/mysql` 报
`requires go >= 1.24.0 (running go 1.22.4)`。

**原因**：驱动最新版提高了最低 Go 要求。本机是 Go 1.22.4。

**解决**：显式指定版本
`go get github.com/go-sql-driver/mysql@v1.8.1`、
`github.com/microsoft/go-mssqldb@v0.20.0`、
`github.com/sijms/go-ora/v2@v2.7.20`。

**结论**：**升级 Go 时要同步升驱动**，否则构建直接断。go.mod 里锁定的
版本不要随手改。

---

### C3 · `CGO_ENABLED=0` 是部署目标的一部分，不能破

**理由**：目标是"单个 exe 扔到内网服务器就能跑"。一旦开 CGO，就依赖
C 工具链和动态库，交叉编译也麻烦。

**为什么能守住**：4 个数据库驱动都选了**纯 Go** 实现
（`sijms/go-ora` 尤其有价值 —— 免装 Oracle Instant Client）。

**结论**：引入新依赖前先确认它支持纯 Go。

---

### C4 · 公司代理会吞本地请求

**现象**：`curl http://127.0.0.1:8099` 无响应或超时。

**解决**：`curl --noproxy '*'`

**同类**：Python `requests` 需要 `ProxyHandler({})`。

---

### C5 · git push 报 credential-manager 缺失是噪音

**现象**：

```
"D:/.../git-credential-manager.exe" store: line 1: ... No such file or directory
To https://github.com/a452065754/-fin.git
   6c7c718..660a335  main -> main
```

**结论**：**第一行是噪音**。全局 `credential.helper` 指向了一个已删除的
旧版本 PortableGit 路径，靠缓存凭据兜底。**看 push 结果行判断成功与否**，
不要被第一行吓到。

**其他环境事实**：

- SSH 22 端口在本机不通，**必须走 HTTPS**
- GitHub MCP connector **无建仓权限**（403 `Resource not accessible by
  integration`），建仓要手动在网页端做

### D5 · 文档里的数字要自动生成，不能手写

**现象**：这个项目里文档里的「测试用例数」「代码行数」这类数字，
本轮被 `verify_docs.py` **抓到过期 5 次**（每做完一个功能就过期一次），
每次都要手工同步，很容易忘。

**结论**：**能从代码算出来的数字，一律不要手写**。

- 新增 `tools/gen_status.py`：直接从代码统计测试数、图表类型数、
  接口数、代码行数、环境变量数，写进 `STATUS.md` 的自动区块
- `verify_docs.py` 增加一项检查：重新生成一遍，和文件里的比对，
  不一致就报错

**两个实现上的坑**：

1. **提交数不能放进自动区块**。提交后提交数必然变 → 自检永远失败。
   凡是**提交后必然变化**的量，都不能进自动区块
2. **比较时只取表格行**，不要整块字符串比。文件里标记是
   `<!-- AUTO:STATUS:BEGIN -->`，生成器输出可能是纯标记，
   再加上 CRLF/LF 差异，整块比会产生**假差异**。只比 `|` 开头的行
   就稳定了

**通用经验**：**文档自检工具本身也要测**。我注入了一个假数字
（66 → 99），确认自检真的报 FAIL，才敢信它。PASS 不代表工具有效。

---

## D. 流程与协作

### D1 · 用户的旧认知可能落后于代码现状

**现象**：用户说"当前只做了 SQLite 数据库，其他还没做"。

**真相**：四类驱动（SQLite/MySQL/Oracle/SQL Server）早已实现并在
go.mod 里。用户的说法来自**早期思考流水的原始决策**
（「首期 FRDemo SQLite + 嵌入式，后期扩展数据库」）。

**结论**：**先核实代码现状，再回应**。不要让用户的记忆覆盖已完成的
工作，也不要盲目照做 —— 那样会重复劳动。这一条直接影响了本轮的工作
顺序判断。

---

### D2 · 接手一个没有版本历史的项目，先存底再动手

**现象**：`F:/QDAKE` **完全没有 .git 目录**。任何改动都无法回滚。

**结论**：第一件事是 `git init` + 首次提交，把当前**能跑的状态**钉死。
之后每完成一个功能就提交。

**配套**：`.gitignore` 要排除调试残留（`tmpcheck/` 54M Chrome profile、
`bin/`、`grouplog.txt`）。

---

### D3 · 文档要"只追加 + 定期校验"，否则会变成负资产

**理由**：文档最大的问题不是写得少，而是**写完不更新**，三个月后全是
错的，比没有文档更误导人。

**做法**：

- `PROGRESS` / `LESSONS` / `SKILLS` **只追加**，保留判断演变轨迹
- `STATUS` / `ROADMAP` 是**当前快照**，该覆盖就覆盖
- 写 `tools/verify_docs.py` 在提交前校验：索引链接有效、状态标记合法、
  进度条数与实际提交数一致
- **文档里的数字必须脚本比对代码**，不能凭印象写

**本项目的验证结果**（一次全过）：

| 校验项 | 方法 | 结果 |
|---|---|---|
| API 端点 | 文档提取 → grep 源码 | 4/4 匹配 |
| 命令行参数 | 同上 | 3/3 匹配 |
| `spec.Kind` 清单 | comm 比对双向差集 | 零差异 |
| 测试用例数 | `grep -c "^func Test"` | 24 = 文档所述 |
| 模板/连接数 | 实际扫描 | 1770 / 14 吻合 |
| 文档链接 | 逐个 stat | 无断链 |

---

### D4 · 和第三方产品对不上时，去找它的原始实现做基准对照

**现象**：内置数据解码成功率只有 36%，猜了七八种参数组合（补位字符、
丢字节与否、Deflate 窗口、按行解码、跳空白）都在 27%–40% 之间打转，
**每次都以为快找到了，但都不对**。

**转折点**：意识到 FineReport 的 class 文件就在
`WEB-INF/lib/fine-cbb-11.0.jar` 里 —— 直接反汇编 + 跑起来做 ground truth。

```bash
cd /tmp && mkdir a85x && cd a85x
unzip -o -q /f/tomcat-win64/webapps/webroot/WEB-INF/lib/fine-cbb-11.0.jar \
    "com/fr/stable/Ascii85*"
javap -c -p com/fr/stable/Ascii85InputStream.class   # 看字节码
# 再把它当库直接调用，输出 hex 做逐条比对
```

**结果**：我们的 Go 实现与 Java 原版 **718/718 条载荷字节级完全一致**。
这个结论一出来，方向立刻从"改解码器"变成"数据本身有问题"——如果没做
这个对照，会继续在解码器上瞎改很久。

**结论**：

1. **猜测参数的上限很低，基准对照一次到位**
2. 反汇编是**格式互操作性分析**的正当手段：目的是理解文件格式，不是
   复制代码
3. 基准工具要**留在项目里**（`tools/oracle/Ascii85Oracle.java`），
   下次改解码器可以立刻回归验证

**推广**：本项目凡是"和 FineReport 行为对不上"的问题，都应该先问
「FineReport 自己的代码在哪」。

---

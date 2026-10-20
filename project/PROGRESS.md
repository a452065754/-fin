# 进度日志

> **只追加，不删改历史**。保留判断演变的轨迹比只留结论有用。
> 每次功能完成后追加一条。格式见文末模板。

---

## 2026-10-20 · 第 1 轮 · 项目从零起步到功能补全

这一轮从「接手一个做了一半的项目」开始，完成了 4 个阶段。起因是用户
明确要求：**做好产品规划，每个功能完成后总结经验教训、沉淀技能、更新
进度、建立索引，避免换人/换 AI 后又要通读分析项目浪费时间**。

### 阶段 A · 项目接手与存底（提交 `0d41295`）

**做了什么**

- 通读 `F:/QDAKE` 现有代码（14 个 Go 文件 + 前端），确认项目性质：
  纯 Go 实现的 FineReport 模板离线查看器
- 实测跑通原有 6 个测试，确认底层是通的，**不是半成品而是覆盖面没收尾**
- `git init` + 首次提交存底（原项目**没有任何版本历史**，改动无回滚兜底）
- 写 `.gitignore` 排除 `tmpcheck/`(54M Chrome profile 残留)、`bin/`、
  `grouplog.txt`
- 推送 GitHub（远程由用户指定为 `a452065754/-fin`）

**怎么验证的**

```
go test ./internal/fr/ -v   # 6 例全 PASS
git ls-remote origin        # 确认远程已有提交
```

**关键决策**

- 项目**没有 git** 是最大风险，先存底再改代码
- 用户原本要建 `reportviewer` 仓库，但 GitHub MCP connector 无建仓权限
  （403），改由用户手动建 `-fin` 仓库

---

### 阶段 B · 多数据连接（提交 `9d11d7e`）

**做了什么**

- 定位数据连接配置真源：`WEB-INF/embed/finedb/db.script`（**HSQLDB** 文本
  格式，不是 H2），表 `FINE_CONF_ENTITY`，键
  `ConnectionConfig.connections.<名字>.<字段>`，共 582 条
- 本机实例共 **14 个连接**，覆盖 sqlite/mysql/oracle/sqlserver 四类，
  含真实业务库（MESCS、mestar、U8、考勤、气密检测等）
- 引入 4 个**纯 Go** 驱动保持 `CGO_ENABLED=0`：
  `modernc.org/sqlite` / `go-sql-driver/mysql` / `sijms/go-ora/v2` /
  `microsoft/go-mssqldb`
- `DataResolver` 从「单 sqlite 全局句柄」改为按需连接池句柄
  `handles map[string]*connHandle`
- 新增 `/api/connections` 接口 + 前端「数据源」面板
- 口令**不读 FineReport 的加密存储**，改用环境变量
  `FINE_PWD_<连接名>`（中文名用 UTF-8 十六进制）
- 测试从 6 例增至 15 例

**怎么验证的**

```
go test ./internal/fr/ -v                    # 15 例全 PASS
curl http://127.0.0.1:8099/api/connections  # 14 个连接，变量名正确
curl ".../api/preview?path=demo/basic/分组统计.cpt"  # FRDemo 渲染与基线一致
```

**未完成的**

- **真库未联调**（本机不在内网，`192.168.0.27:3306` 等端口全不通）。
  DSN 构造与错误路径有用例覆盖，真连通需到内网机器验证。已记入 ROADMAP P0-3。

---

### 阶段 C · 补齐图表类型（提交 `6c7c718`）

**做了什么**

- 扫 1770 个 CPT 统计出 **21 种 Plot 类**，原先只映射 8 种
- 扫描发现真正的瓶颈不是类型映射，而是 **`ChartDefinition` 有 12 种数据
  定义 681 处，原先只处理 2 种**
- 新实现 6 类定义：`BubbleTableDefinition201109`（X/Y/半径三轴）、
  `CustomDefinition`（组合图，每系列图型取自 `CustomPlotList`）、
  `VanGanttTableDefinition`（起止区间 + 子系列）、
  `StructureTableDefinition`（父子层级）、
  `VanMapDefinition`/`DillMapDefinition`
- 新增 7 种 `spec.Kind`：combo / bubble / gantt / sunburst / multipie /
  heatmap / box
- 前端 `chartOption` 加 7 个渲染分支
- 修掉 3 个隐蔽 bug（见 LESSONS）
- 测试从 15 例增至 24 例

**怎么验证的**

```
go test ./internal/fr/ -v                      # 24 例全 PASS
curl ".../api/preview?path=doc/Chart/GanttChart/关联线甘特图.cpt"
  → kind=gantt, 9 系列, 数据点 9（需求分析 18 天 / 详细设计 46 天）
curl ".../api/preview?path=demo/chart/DataVisualization/数据可视化.cpt"
  → kind=bubble, 12 点, X=成本价 Y=单价, 半径 29.2/13.7/23.6
curl ".../api/preview?path=demo/analytics/Demo.cpt"
  → kind=combo, 数量=bar(6点) + 辅助=line(6点)
```

**未完成的**

- `MeterTableDefinition` / `MeterReportDefinition` / `NormalReportDataDefinition`
  三类定义未实现 → ROADMAP P0-1 / P0-2
- 地图/词云/箱型图降级 → ROADMAP P1-1 / P1-2 / P1-3

---

### 阶段 D · 文档体系（提交 `660a335`）

**做了什么**

- `docs/` 四份文档：部署 / 使用 / 开发 / 交接
- README 重写为总入口，加文档索引表
- 写 `tools/verify_docs.py` 防止文档腐化

**怎么验证的**

- 脚本逐项比对代码：4 个 API 端点、3 个 flag、18 种 `spec.Kind` 清单
  （comm 比对零差异）、24 个测试用例、1770 模板、14 连接、20000 行上限、
  `MaxOpenConns(2)`、7445 行 Go 代码 —— 全部吻合
- 全部 markdown 链接与文件引用有效性校验通过
- `gofmt` / `go vet` / `go test` / `node --check` 全部通过

---

### 阶段 E · 知识库与产品规划（提交 `待提交`）

**做了什么**

用户提出：**「做好产品规划，每个功能完成测试 OK 后总结经验教训、技能沉淀、
进度更新、做好索引，不要中断或更换 AI 后又要通读分析项目浪费时间」**。

- 建立 `project/` 知识库六件套：
  - `INDEX.md` —— 唯一入口，含 5 分钟上手步骤、工作流程、状态标记规范、
    10 条项目铁律
  - `STATUS.md` —— 一页速览快照（能做什么/不能做什么/硬指标/风险）
  - `ROADMAP.md` —— 产品路线图，4 个阶段 + 优先级 + **验收标准** + 依赖
  - `PROGRESS.md` —— 只追加工作日志，回填 A/B/C/D 四阶段
  - `LESSONS.md` —— 只追加经验库，13 条，每条含「现象→根因→结论」
  - `SKILLS.md` —— 只追加技能库，5 类可复用方法与命令模板
- 写 `tools/verify_docs.py` —— 7 类文档自检：
  相对链接 / 源码引用 / 状态标记 / 关键数字 / 只追加完整性 / 进度与提交
  一致性 / 索引完整性
- README 与 docs/开发.md、docs/交接.md 全部加知识库入口

**怎么验证的**

- **注入故障验证工具有效性**（关键：PASS 不代表工具有用）：
  - 注入断链 → 捕获 ✅
  - 文档少写一种 `spec.Kind` → 捕获 ✅
  - 文档引用不存在的源码文件 → 捕获 ✅
  - 三种故障恢复后重新 PASS ✅
- 修复工具自身的重复报错（同一断链报两次）

**关键决策**

- **文件名用英文**（`project/STATUS.md` 而非 `project/状态.md`）：
  本机 Python 对中文文件名的处理在 Git Bash 下有编码坑（实测
  `FileNotFoundError`），用英文名彻底规避
- **LESSONS 与 SKILLS 分开**：前者记「踩过的坑」，后者记「下次直接抄的」。
  混在一起会导致查找时找不到想要的东西
- **每条 LESSON 必须写「怎么发现的」**：只写结论没用，下次遇到同样现象
  还是不知道从哪查起

**遗留**

- 无。知识库已建立并接入流程。

---

### 阶段 F · 仪表盘数据定义 + 内置数据根因定位（提交 `待提交`）

**做了什么**

P0-1 仪表盘数据定义：

- 解析 `MeterTableDefinition`（`MeterTable201109` 的 `meterType`/`name`/
  `value`/`custom`/`customTarget` 属性）
- 解析仪表盘量程与阈值分段：量程在 Plot 的 `MC_Attr`
  （`minValue`/`maxValue`/`useType`），分段在 `AC_Attr`（阈值 + 颜色）
- 修了 `AC_Attr` 的两种形态：颜色在 `color` 属性里（140 处）或在
  `<color><FineColor/></color>` 子节点里（18 处）
- 修了分段范围倒挂：158 个分段里 56 个是 `min > max`，需归一化
- 前端 `chartOption` 加仪表盘渲染：按模板声明的量程与色带画，
  不再用「按值猜量程」；分段超出量程时自动放宽量程
- 无数据的仪表盘**不画**（返回 null），避免把「取不到数」画成 0

P0-4 内置数据根因定位（顺带做的，因为仪表盘取不到数）：

- 确认载荷解码成功率 36%（718 段中 454 段失败）
- 逐一排除 7 种假设（补位字符 / 丢字节 / Deflate 窗口 / 按行解码 /
  跳空白 / 标准 a85decode / 单字符损坏）—— 全部不解决
- **决定性做法**：找到 `WEB-INF/lib/fine-cbb-11.0.jar` 里的
  `com.fr.stable.Ascii85InputStream`，`javap` 反汇编 + 直接当库调用，
  做 ground truth 比对
- **结论：我们的 Go 解码器与 FineReport 原版 718/718 条载荷字节级完全
  一致**；Java 原版对同一载荷做 Inflater 也抛
  `ZipException: invalid distance code`
- 即：**失败是模板文件自身的数据问题，不是解码器问题**
- 基准工具存入 `tools/oracle/Ascii85Oracle.java` 供后续回归

**怎么验证的**

```
go test ./internal/fr/ -v              # 27 例全 PASS（原 24 + 仪表盘 3）
curl /api/preview?path=demo/chart/basic/多个仪表盘.cpt
  → gauge 值=[0.75, 0.15, 0.1] 范围=[0,100] 段数=3
     段 [0.2,0.75] #50d2e5 / 段 [0.15,0.2] #2190b6 / 段 [0,0.15] #07578f
  → gauge 值=[0.5] 范围=[0,100]；值=[0.25] 范围=[0,100]
  → 2 个仪表盘无数据（对应载荷损坏，告警已说明）
```

**关键决策**

- 仪表盘无数据时**不画**而不是画 0 —— 画 0 是撒谎
- `gaugeMin` 去掉 `omitempty` —— 0 是合法量程下限，前端需要区分
  「下限是 0」与「未知下限」
- 把 Java 基准工具**留在项目里** —— 下次改解码器可立刻回归

**遗留**

- 载荷在 Ascii85 之前是否还有一步处理（加密/混淆）未查清，需看
  FineReport 写 RowData 的那层代码 → ROADMAP P0-4
- `MeterReportDefinition`（引用单元格版仪表盘）未实现 → ROADMAP P0-2

---

---

### 阶段 G · 引用单元格的数据定义（提交 `待提交`）

**做了什么**

P0-2 引用报表单元格的图表（82 处定义）：

- 新增 `internal/fr/chartref.go`：解析 `NormalReportDataDefinition` /
  `BubbleReportDefinition` / `MeterReportDefinition` 里的单元格引用
  （`=C3`、`$B$4`、区间 `=B2:B9` 取首格）
- 引用解析**两遍渲染**：第一遍渲染出网格，第二遍把引用单元格的值填进图表
  （图表在渲染时拿不到其它单元格的最终值）
- 修 `SeriesDefinition` 查找层级：它在 `<Series>` 下面，**不是直接子节点**，
  要用 `FindAll` 而不是 `ChildAll`

**顺带修掉一个长期 bug（影响面比图表大得多）**：

- **数据集自身的参数默认值从来没生效过**。模板的每个数据集可以自带
  `<Parameters>`（如「地区=华东」），但 `Dataset.Load` 只把请求参数传进
  `ExpandSQL`，数据集默认值被忽略 → 请求没给参数时 SQL 展开成空值 →
  查不到行 → 整个模板的单元格都是空的
- 修复：`Dataset` 加 `Params []ParamDef`，`parseDatasetParams` 解析，
  `effectiveParams` 按「请求参数 > 数据集默认值 > 空」合并
- **影响所有带数据集参数的模板**，不止图表

**怎么验证的**

```
go test ./internal/fr/ -v      # 33 例全 PASS（原 28 + 新增 5）
curl /api/preview?path=GettingStarted.cpt
  → chart kind=bar cats=['孙林'] series=1
     'C4' -> [140]              # 140 = 孙林 苹果汁 的销量，取自报表单元格
```

新增用例：数据集自身参数、参数合并优先级、模板带参数仍能渲染、
引用单元格的图表、引用解析的各种写法。

**关键决策**

- 引用单元格的图表采用**两遍渲染**，而不是把单元格值提前算好塞进 context
  —— 单元格值依赖展开结果，只有渲染完才知道
- 系列名优先取「系列名单元格」的值，取不到再退到「值单元格」的值，
  最后才用单元格引用本身（避免图表系列名显示成 "C4"）

**遗留**

- 仪表盘报表版（`MeterReportDefinition`）走同一套引用机制，已随之支持
- 词云 / 箱型图 / 地图见 ROADMAP P1-x

---

---

### 阶段 H · 词云图表（提交 `待提交`）

**做了什么**

P1-2 词云（3 处图表）：

- 后端：解析 `WordCloudTableDefinition`（`wordname` 词列 / `wordvalue`
  权重列），产出「分类=词、系列=权重」的结构
- 前端：**不引第三方插件，自研行填充布局**
  - 原因：`echarts-wordcloud` 要求 ECharts ^5.0.1，项目打包的是 4.1.0；
    升级 ECharts 回归面太大（影响所有图表）
  - 布局：大词优先 → 逐行摆放 → 行内按字号排序 → 每行居中
  - 用 scatter + label 渲染，字号按权重映射
- 新增 `tools/tests/wordcloud_layout_test.mjs`：把 `layoutWordCloud` 从
  app.js 抽出来用 Node 跑，断言「全部放置 / 零重叠 / 零越界 / 字号合法」

**怎么验证的**

```
go test ./internal/fr/ -v                              # 36 例全 PASS（原 33 + 词云 3）
node tools/tests/wordcloud_layout_test.mjs
  PASS n=10 放置=10/10 重叠=0 越界=0 非法字号=0 高度=63
  PASS n=40 放置=40/40 重叠=0 越界=0 非法字号=0 高度=187
  PASS n=80 放置=80/80 重叠=0 越界=0 非法字号=0 高度=338
```

**关键决策**

- **不升级 ECharts**。4.1.0 → 5.x 会影响所有图表的渲染，回归面远大于
  3 处词云的收益。自己写 60 行布局更划算
- **行填充而不是螺旋**。螺旋路径浪费空间（实测只填满 18%），行填充
  放置率 100%、零重叠

**踩到的坑**

- 行内 x 坐标忘了累加，导致同一行的词全挤在一起（dx=0.4）。**目视发现不了，
  是靠脚本量化查重叠才暴露的** → 布局算法必须写自动化断言
- 字号在权重区间退化时可能算出负数 → 夹紧到 [10, 48]

**遗留**

- demo 模板的词云数据本身解不出来（deflate 流不完整，FineReport 自己的
  Inflater 也是 not-finished），所以端到端看到的是空图；数据路径用合成
  数据做了覆盖

---

---

### 阶段 I · 箱型图（提交 `待提交`）

**做了什么**

P1-3 箱型图（3 处图表）：

- 后端：解析 `BoxTableDefinition`（内含 `detailedDefinition` 存放原始值、
  `resultDefinition` 存放五数概括的标签），从原始值算出
  `[min, q1, 中位数, q3, max]`
- 新增 `fiveNumberSummary`：先排序，再用线性插值算分位数
  （q1=0.25 分位、median=0.5、q3=0.75）
- 模型：`ChartSeries` 加 `BoxValues [][]float64`（每个分类一组五数概括）
- 前端：ECharts `boxplot` 系列 + 五数概括的 tooltip

**怎么验证的**

```
go test ./internal/fr/ -v   # 39 例全 PASS（原 36 + 箱型图 3）
  系列 "普通茉莉花" 箱体: [[10 12 14 16 18] [5 6 7 8 9]]
  系列 "宝珠茉莉花" 箱体: [[20 22 24 26 28] [8 9 10 11 12]]
```

新增用例：五数概括的数值（乱序输入、单值、空集）、箱型图的分类与系列
数量、箱体单调性、端到端类型识别。

**关键决策**

- **五数概括不能塞进 `Data []float64`**（一个数放不下 5 个值），
  所以给 `ChartSeries` 单独加了 `BoxValues` 字段，而不是想办法编码
- demo 模板的箱型图数据同样解不出来（deflate 流不完整），所以数据路径
  用合成数据集覆盖，类型识别用真实模板验证

**遗留**

- 地图 GeoJSON 需离线地理数据，见 ROADMAP P1-1

---

## 进度条目模板

```markdown
## YYYY-MM-DD · 第 N 轮 · <主题>

**做了什么**
- <具体到文件与函数>

**怎么验证的**
- <可复现的命令或步骤 + 实际观察到的结果>

**关键决策**
- <为什么这么做，放弃了什么方案>

**遗留 / 阻塞**
- <转写到 ROADMAP 的具体编号>
```

---

## 历史统计

| 轮次 | 日期 | 阶段 | 提交 | 测试 |
|---|---|---|---|---|
| 1 | 2026-10-20 | A 存底 | `0d41295` | 6 → 6 |
| 1 | 2026-10-20 | B 多数据连接 | `9d11d7e` | 6 → 15 |
| 1 | 2026-10-20 | C 图表类型 | `6c7c718` | 15 → 24 |
| 1 | 2026-10-20 | D 文档体系 | `660a335` | 24 → 24 |
| 1 | 2026-10-20 | E 知识库与规划 | `01e0e1f` | 24 → 24 |
| 1 | 2026-10-20 | F 仪表盘 + 数据根因 | `a7f72ae` | 24 → 28 |
| 1 | 2026-10-20 | G 引用单元格 | `98d8020` | 28 → 33 |
| 1 | 2026-10-20 | H 词云 | `62ba1dd` | 33 → 36 |
| 1 | 2026-10-20 | I 箱型图 | 本次 | 36 → 39 |

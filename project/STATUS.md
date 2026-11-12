# 当前状态快照

> 一页速览。**每个功能完成后更新本文件**（覆盖式，不是追加）。
> 最后更新：2026-11-12 16:20（地图区域按官方配色连成整块、点击联动改为局部刷新）

## 一句话

FineReport 11.0 的 `.cpt`/`.fvs` 模板离线查看器。纯 Go 单 exe，读本机
webroot 渲染成 HTML 预览。内网无外网环境可直接部署。

## 能不能做

| 场景 | 能？ | 说明 |
|---|---|---|
| 看普通报表（CPT 表格） | ✅ | 分组/汇总/合并/横向分栏都正常 |
| 看含公式的报表 | ✅ | 求值器 1988 行，覆盖 60+ 函数 |
| 看大屏（FVS） | ✅ | 文本/图片/图表/报表块/Tab 都能渲染 |
| 看柱/线/面/饼/漏斗/散点/雷达图 | ✅ | |
| 看组合图/气泡图/甘特图/层级图/热力图/多层饼 | ✅ | |
| **看仪表盘** | ✅ | 数值、量程、阈值分段色带都对 |
| **看引用报表单元格的图表** | ✅ | `=C3` 这类公式引用的图表能取到值 |
| **看词云** | ✅ | 自研行填充布局，无需 ECharts 5 插件 |
| **看箱型图** | ✅ | 从原始值算五数概括 |
| **FVS 渲染细节对齐官方** | ✅ | 图例显隐/位置、饼图内标签与实心/圆环、模板系列配色、地图滚轮缩放与完整居中、横条柱宽、表头与文本不裁切 |
| 查 FRDemo 演示库 | ✅ | 免密 |
| 查 MySQL / Oracle / SQL Server | ⚠️ | 代码就绪，**未连真库验证** |
| 看地图 | ✅ | 内置省级离线 GeoJSON，区域着色、滚轮缩放、自动填满居中；市/区级钻取仍待补 |
| 看填报报表（表单） | ✅ | 16 万控件单元格渲染为可交互控件 |
| 看 H5 手机端模板 | ✅ | 自动切手机布局 |

## 硬指标

<!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- <!-- AUTO:STATUS:BEGIN -->

<!-- 本区块由 tools/gen_status.py 生成，请勿手改。改完跑一次即可。 -->

| 指标 | 值 | 怎么测的 |
|---|---|---|
| 测试用例 | 126 例 | `go test ./...` |
| 图表类型 | 18 种 | `internal/fr/chart.go` 的 `spec.Kind` 枚举 |
| HTTP 接口 | 7 个 | `internal/server/server.go` 的路由表 |
| Go 代码 | 16464 行 | `wc -l` |
| 前端代码 | 4436 行 | `wc -l` |
| 可配置环境变量 | 5 个 | `FR_TEST_WEBROOT`、`FR_WEBROOT`、`RV_AUDIT_LOG`、`RV_PASSWORD`、`RV_USER` |

<!-- AUTO:STATUS:END --> --> --> --> --> --> --> --> --> --> --> --> --> --> -->

以下几项依赖运行环境，手工维护：

| 指标 | 值 | 怎么测的 |
|---|---|---|
| 模板扫描 | 1770 个 CPT+FVS | `GET /api/templates` |
| 数据连接发现 | 14 个（sqlite/mysql/oracle/sqlserver） | `GET /api/connections` |
| Ascii85 解码一致性 | **718/718 与 FineReport 原版字节级一致** | Java oracle 逐条比对 |
| 单文件体积 | 约 25 MB（含 4 个纯 Go 驱动） | `ls -la bin/` |

> 刷新上面的自动区块：`python3 tools/gen_status.py`
| 运行时依赖 | 0 | 无 Node/Python/JRE/DB 客户端 |

## 本轮（2026-11-12 · 第 5 轮）做了什么

用户第三轮截图，3 个问题都集中在地图联动上：

| 问题 | 根因与修法 |
|---|---|
| 华东被切成好几块、区域颜色不对 | `visualMap` 把所有区域染成同一个亮蓝 `#1CA7FF`，且省界描边把区域切碎。改为**按区域固定配色**（官方华北 `#7baac7`、华东 `#1f84c5`），每个成员省份同色 + `borderWidth:0` 隐藏内部省界；删掉 visualMap；`symbolSize:0` 去掉 ECharts 在每个省中心画的青色小圆点。区域按**名称**排序分配颜色，联动筛选后颜色保持稳定 |
| 点击地图整屏闪一下 | `fvsLinkageClick` 走 `loadPreview` → `showLoading` + 隐藏/重建整个 DOM。改为 `refreshLinkedWidgets` + `updatePageInPlace`：只请求新数据，按 widget id 原地 `setOption` / 重建表格 / 刷新文本 |
| 点击后组件位置全变 | 整页重建会重跑 `applyZoom`、重新 init 地图。局部刷新后 DOM 与 `page-scale` transform 不变，并保存/恢复地图的 `zoom` / `center` |

验证（CDP 模拟点击华北）：`sameDom=true`（首个 widget 仍是同一 DOM 节点）、
`pageTransform` 点击前后均为 `scale(0.967187)`、`mapZoom=1.5` 与 center 不变；
标题变为「华北销售人员销量信息 / 华北区域销售详情 / 华北区域各产品销量」，
表格切换为华北 4 人，URL 追加 `area=华北`；地图 `dotCount=0`、无 visualMap。

## 第 4 轮（2026-11-12）做了什么

用户第二轮截图（前官方 / 后查看器）又提 5 处差异，全部修完：

| 差异 | 根因与修法 |
|---|---|
| 饼图被画成空心圆环 | 模板 `PieAttr4VanChart innerRadius="0.0"` 表示**实心饼**，前端却写死 `radius:['30%','62%']`。新增 `PieInnerRadius`，按模板值生成 radius |
| 柱形图图例在左（官方在右） | 上一轮把 VanChart `position=4` 误映射为 left。恢复 `4=right / 2=left`，以官方截图为准 |
| 柱形图 5 个系列全蓝 | `parseChartColors` 把 `colvalue` 当**属性**读，但模板是 `<OColor><colvalue><FineColor color=.../></colvalue></OColor>` 子节点链，所以调色板永远为空、退回全蓝 DARK_PALETTE。改为读子节点，得到官方青/蓝/黄/橙/绿 |
| 横向柱图蓝色（官方青色） | 同一根因：调色板第 1 色应为 `#00d0f5`，修好解析后自动生效 |
| 地图默认显示不全 | 上一轮 `zoom:2` 放大过头，南北省份被容器裁掉。降到 `1.5`（填满高度且不溢出） |

验证：Edge 无头截图逐项比对；`/api/preview` 实测 4 个图表都带官方
`colors=['#00d0f5','#1a97ff','#ffd87b','#ff8249','#7ecf93',...]`、
柱图 `legendPos=right`、饼图 `radius='62%'`、地图 `zoom=1.5`；
新增 3 个回归测试（123 → 126 例）全绿，`tools/verify_docs.py` PASS。

## 上一轮（2026-11-11）做了什么

对照 FineReport 官方截图，修完 `第一张FVS模板.fvs` 预览的 7 处渲染差异
（详见 [PROGRESS.md](PROGRESS.md) 第 3 轮）：

| 差异 | 修法 |
|---|---|
| 图例该隐藏/该在左 | `parseChartLegend` 取 `<Legend>` 的**直接** `<Attr>` 子节点（原来递归 `Find` 命中 `<GI><AttrBackground>` 里的无关 Attr）；修正 VanChart 位置枚举 |
| 饼图标签跑扇区外 | 新增 `LabelInside`，读 `AttrLabel > labelDetail > Attr.showLine`，无牵引线 ⇒ `position:'inside'` |
| 地图不能滚轮缩放 / 太小且偏右 | `zoomAttr.zoomGesture` 驱动 `roam`；GeoJSON 绝对经纬度归一化到原点 + 几何中心 + 2 倍拟合缩放 |
| 大数字上方缺「华北/华东」 | 文本组件改用单个 wrapper 承载富文本、清理尾部 `<br>`，避免 flex 列把 `<br>` 变成独立项撑高后被裁 |
| 横向柱图柱子过粗 | `barWidth:'25%'` 取代 36px 上限 |
| 表头「销售员」被裁成两行 | 单元格文本去掉富文本前导/尾随换行 |
| 「点击查看FVS详情」被挤换行 | `white-space:nowrap` + `overflow:visible`（FR 允许文字溢出框） |

验证：Edge 无头浏览器逐项截图比对官方图；新增 5 个回归测试（图例嵌套 Attr 陷阱、
可见图例、饼图内标签、地图缩放开/关）全绿。

## 上一轮（2026-10-20）做了什么

一轮做完 4 个阶段，4 个提交：

| 阶段 | 内容 | 提交 |
|---|---|---|
| A | git 存底 + 推送 GitHub | `0d41295` |
| B | 多数据连接（SQLite/MySQL/Oracle/SQL Server） | `9d11d7e` |
| C | 补齐图表类型（7 类新增） | `6c7c718` |
| D | 文档体系（4 份文档 + 知识库） | `660a335` |
| E | 知识库与产品规划 | `01e0e1f` |
| F | **仪表盘数据定义 + 内置数据根因定位** | 本次 |

## 已知阻塞

| 项 | 影响 | 解法 |
|---|---|---|
| 本机不在内网，数据库端口全不通 | 真库无法端到端验证 | 代码路径已覆盖，**需到内网机器验证** |
| FVS 内置数据约 64% 解不出 | 部分大屏数据缺失 | **已证明不是解码器问题**（与 Java 原版 718/718 一致），是文件数据本身的问题，见 ROADMAP P0-4 |
| 仪表盘报表版（`MeterReportDefinition`）未实现 | 引用单元格的仪表盘 | 见 ROADMAP P0-2 |

## 下一步

省级离线地图已落地并能真实着色/缩放；剩余是市/区级钻取 GeoJSON，
之后是第 2 阶段生产化（访问控制、超时熔断、健康检查、优雅停机）与 P2-x 增强项。
详见 [ROADMAP.md](ROADMAP.md)。

## 风险提示

| 风险 | 说明 |
|---|---|
| 多数据源未真机验证 | 首次上内网时**先验证连接**，DSN 构造可能有方言细节问题 |
| 依赖版本锁死在 Go 1.22 兼容线 | 升级 Go 时要同步升 mysql/mssql 驱动，否则报 `requires go >= 1.24` |
| 无 CI | 测试全靠本地跑，换机器容易漏 |

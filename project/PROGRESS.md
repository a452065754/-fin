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
| 1 | 2026-10-20 | E 知识库与规划 | 本次 | 24 → 24 |

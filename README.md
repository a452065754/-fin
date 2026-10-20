# reportviewer

纯 Go 实现的 FineReport 11.0 报表模板离线查看器。读取本机 FineReport webroot，
把 `.cpt` / `.fvs` 模板渲染成 HTML 预览。

- **零运行时依赖**：只用 Go 标准库 + 纯 Go 数据库驱动，编译为单个 exe，
  内网服务器无需装 Node、Python 或任何数据库客户端。
- **无 CGO**：`CGO_ENABLED=0`，交叉编译方便，产物自包含。

## 文档

### 先看这里

| 文档 | 读者 | 内容 |
|---|---|---|
| [project/INDEX.md](project/INDEX.md) | **所有人** | **项目索引与工作流程（唯一入口）** |
| [project/STATUS.md](project/STATUS.md) | 所有人 | 一页速览：能做什么、不能做什么 |
| [project/ROADMAP.md](project/ROADMAP.md) | 所有人 | 产品规划与任务状态 |

### 专项文档

| 文档 | 读者 | 内容 |
|---|---|---|
| [docs/部署.md](docs/部署.md) | 运维 / 实施 | 安装、配置、启动、验证、排障、卸载 |
| [docs/使用.md](docs/使用.md) | 业务用户 / 实施顾问 | 界面操作、HTTP 接口、图表支持范围 |
| [docs/开发.md](docs/开发.md) | 开发 / AI 接手 | 架构、模块约定、加新类型的流程、测试规矩 |
| [docs/交接.md](docs/交接.md) | 接手人 | 项目全貌、接手须知、踩坑清单 |

### 过程记录

| 文档 | 性质 | 内容 |
|---|---|---|
| [project/PROGRESS.md](project/PROGRESS.md) | 只追加 | 每次做完了什么、怎么验证的 |
| [project/LESSONS.md](project/LESSONS.md) | 只追加 | 踩过的坑与结论 |
| [project/SKILLS.md](project/SKILLS.md) | 只追加 | 可复用的方法与命令模板 |

> 第一次接手请先读 **[project/INDEX.md](project/INDEX.md)**，它有 5 分钟上手
> 步骤和项目铁律。

### 工作流程

每个功能完成后必须更新知识库（STATUS / ROADMAP / PROGRESS / LESSONS /
SKILLS），提交前跑文档自检：

```bash
python3 tools/verify_docs.py
```

## 快速开始

```bash
go build -o bin/reportviewer.exe ./cmd/reportviewer
./bin/reportviewer.exe                    # 默认 :8090
./bin/reportviewer.exe -addr :9000         # 换端口
FR_WEBROOT=D:\tomcat\webapps\webroot ./bin/reportviewer.exe   # 指定 webroot
```

启动后浏览器打开提示的地址。左侧是模板树（支持搜索与 CPT/FVS 过滤），
右侧是渲染结果。

## 架构一览

```
cmd/reportviewer/main.go     入口：flag、webroot 探测、HTTP 启动
internal/server/server.go    HTTP 层：4 个 API、路径白名单、模板树扫描
internal/fr/                 解析与渲染核心（约 5000 行，不依赖 HTTP）
  cpt.go / fvs.go            模板解析
  dataset.go / connection.go 数据集与数据连接
  embedded.go                内置数据解码（Ascii85 + raw Deflate）
  formula.go / render.go     公式求值与表格渲染
  chart.go / style.go        图表与样式
web/                         内嵌前端（go:embed）+ 本地 echarts
```

## 支持的模板能力

| 能力 | 状态 |
|---|---|
| CPT 表格（分组、汇总、合并单元格、跨页分栏） | ✅ |
| CPT 公式（1193 行求值器） | ✅ |
| CPT 图表（柱/线/面/饼/漏斗/散点/雷达/仪表） | ✅ |
| 组合图（每个系列独立图型 + 双轴） | ✅ |
| 气泡图（X/Y/半径三轴） | ✅ |
| 甘特图（起止时间区间，支持子系列） | ✅ |
| 层级图（父子结构 → 旭日图 / 矩形树图） | ✅ |
| 多层饼图（同心环） | ✅ |
| 热力图 | ✅ |
| 词云（自研行填充布局） | ✅ |
| 箱型图（从原始值算五数概括） | ✅ |
| 仪表盘（数值 + 量程 + 阈值色带） | ✅ |
| FVS 画布（文本/图片/图表/报表块/Tab 容器） | ✅ |
| 数据源：内置离线数据（Ascii85+Deflate 解码） | ✅ |
| 数据源：FRDemo SQLite | ✅ |
| 数据源：MySQL / Oracle / SQL Server | ✅ 需配密码，见下 |

### 已知降级

以下情况不会中断渲染，而是在「渲染提示」面板里给出警告：

- 地图图 → 降级为排名条形图（未内置省市区地理数据，需要离线 GeoJSON 才能画出真地图）
- FVS 内嵌数据流被截断 → 使用可解码的部分
- 未识别的 FVS 控件类型 → 跳过并告警

## 数据连接

程序启动时会读取 `WEB-INF/embed/finedb/db.script`（FineReport 的 HSQLDB 配置库），
自动发现该实例配置的所有数据连接。支持的类型：

| 类型 | 驱动 | 说明 |
|---|---|---|
| SQLite | `modernc.org/sqlite` | FRDemo 随产品自带，无需密码 |
| MySQL | `go-sql-driver/mysql` | 纯 Go |
| Oracle | `sijms/go-ora/v2` | 纯 Go，无需 Instant Client |
| SQL Server | `microsoft/go-mssqldb` | 纯 Go |

### 配置密码

FineReport 自己存的口令是加密的，本程序**不读取、不破解**它。
口令通过环境变量注入，命名规则见程序内「数据源」面板：

```
FINE_PWD_<连接名>          连接名为纯 ASCII 时，如 MESCS → FINE_PWD_MESCS
FINE_PWD_<UTF-8 十六进制>    连接名含中文时，如 气密检测77 → FINE_PWD_E6B094E5AF86E6A380E6B58B3737
```

设置后重启程序生效。点击左下角「数据源」可以看到每个连接的可用状态，
以及它需要的环境变量名。

### 安全约束

- **只读**：查询前强制校验语句必须以 `SELECT` 或 `WITH` 开头，
  拒绝一切 DDL/DML。
- **连接数**：每个数据源最多 2 个连接，避免预览操作耗尽源库连接池。
- **行数上限**：单次查询最多取 20000 行。
- **不出网**：程序不主动外联；口令不写日志、不返回给浏览器。

## 开发

```bash
export GOCACHE=$(pwd)/.gocache    # Windows 下若默认缓存目录不可写
go test ./...                     # 102 例
```

测试依赖本机存在 FineReport webroot（默认 `F:/tomcat-win64/webapps/webroot`）。
webroot 不存在时相关用例会 skip 而非失败。

提交前检查：

```bash
gofmt -l . && go vet ./... && go test ./... && node --check web/app.js
python3 tools/verify_docs.py
```

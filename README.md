# reportviewer

纯 Go 实现的 FineReport 11.0 报表模板离线查看器。读取本机 FineReport webroot，
把 `.cpt` / `.fvs` 模板渲染成 HTML 预览。

- **零运行时依赖**：只用 Go 标准库 + 纯 Go 数据库驱动，编译为单个 exe，
  内网服务器无需装 Node、Python 或任何数据库客户端。
- **无 CGO**：`CGO_ENABLED=0`，交叉编译方便，产物自包含。

## 快速开始

```bash
go build -o bin/reportviewer.exe ./cmd/reportviewer
./bin/reportviewer.exe                    # 默认 :8090
./bin/reportviewer.exe -addr :9000         # 换端口
FR_WEBROOT=D:\tomcat\webapps\webroot ./bin/reportviewer.exe   # 指定 webroot
```

启动后浏览器打开提示的地址。左侧是模板树（支持搜索与 CPT/FVS 过滤），
右侧是渲染结果。

## 支持的模板能力

| 能力 | 状态 |
|---|---|
| CPT 表格（分组、汇总、合并单元格、跨页分栏） | ✅ |
| CPT 公式（1193 行求值器） | ✅ |
| CPT 图表（柱/线/面/饼/漏斗/散点/雷达/仪表） | ✅ |
| FVS 画布（文本/图片/图表/报表块/Tab 容器） | ✅ |
| 数据源：内置离线数据（Ascii85+Deflate 解码） | ✅ |
| 数据源：FRDemo SQLite | ✅ |
| 数据源：MySQL / Oracle / SQL Server | ✅ 需配密码，见下 |

### 已知降级

以下情况不会中断渲染，而是在「渲染提示」面板里给出警告：

- 地图、组合图、气泡图、多层饼图 → 降级为柱形图
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
go test ./...
```

测试依赖本机存在 FineReport webroot（默认 `F:/tomcat-win64/webapps/webroot`）。
webroot 不存在时相关用例会 skip 而非失败。
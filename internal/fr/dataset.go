package fr

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	// Pure-Go drivers keep the build CGO-free so reportviewer.exe stays a
	// single self-contained binary on the intranet server.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
)

// Dataset is one entry of the template's TableDataMap.
type Dataset struct {
	Name        string
	ClassName   string
	Conn        string
	Query       string
	Columns     []string
	ColumnTypes []string
	Rows        [][]any
	Loaded      bool
	Err         string
	Warnings    []string
	loadKey     string
}

// DataResolver executes template datasets against the datasources declared in
// the FineReport installation. FRDemo ships with the product; every other
// connection needs its password supplied through the environment.
type DataResolver struct {
	Webroot string

	conns *Connections

	mu      sync.Mutex
	handles map[string]*connHandle
	dicts   map[string]map[string]string
	dictErr map[string]string
}

type connHandle struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

func NewDataResolver(webroot string) *DataResolver {
	dr := &DataResolver{
		Webroot: webroot,
		handles: map[string]*connHandle{},
		dicts:   make(map[string]map[string]string),
		dictErr: make(map[string]string),
	}
	if c, err := LoadConnections(webroot); err == nil {
		dr.conns = c
	} else {
		// Fall back to the bundled demo database so FRDemo keeps working even
		// when the embedded store is unreadable.
		dr.conns = &Connections{items: map[string]*Connection{
			"FRDemo": {Name: "FRDemo", Kind: "sqlite", URL: "jdbc:sqlite://${ENV_HOME}/../help/FRDemo.db"},
		}, order: []string{"FRDemo"}}
	}
	return dr
}

// Connections exposes the parsed datasource catalogue for the UI.
func (dr *DataResolver) Connections() *Connections { return dr.conns }

func (dr *DataResolver) Close() {
	dr.mu.Lock()
	hs := dr.handles
	dr.handles = map[string]*connHandle{}
	dr.mu.Unlock()
	for _, h := range hs {
		if h.db != nil {
			h.db.Close()
		}
	}
}

func (dr *DataResolver) frdemoPath() string {
	if dr.Webroot == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(dr.Webroot, "help", "FRDemo.db"),
		filepath.Join(dr.Webroot, "WEB-INF", "help", "FRDemo.db"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return candidates[0]
}

func (dr *DataResolver) EnsureDB() error {
	_, err := dr.handle("FRDemo")
	return err
}

// handle lazily opens a pooled connection for the named datasource.
func (dr *DataResolver) handle(name string) (*connHandle, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "FRDemo"
	}
	key := strings.ToLower(name)

	dr.mu.Lock()
	h := dr.handles[key]
	dr.mu.Unlock()
	if h != nil && h.db != nil {
		return h, nil
	}

	c := dr.conns.Get(name)
	if c == nil {
		return nil, fmt.Errorf("未找到数据连接 %q（当前可用: %s）", name, strings.Join(dr.names(), ", "))
	}
	if c.NeedsPassword && os.Getenv(c.PasswordEnv) == "" {
		return nil, fmt.Errorf("数据连接 %s 未配置密码，请设置环境变量 %s", c.Name, c.PasswordEnv)
	}

	dsn, err := BuildDSN(c, c.Password(), dr.Webroot)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driverFor(c.Kind), dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据连接 %s 失败: %w", c.Name, err)
	}
	// Datasheets are read-only previews; keep one or two connections so a
	// viewer instance never exhausts the source database pool.
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接数据源 %s 失败: %w", c.Name, err)
	}

	h = &connHandle{db: db, path: dsn}
	dr.mu.Lock()
	dr.handles[key] = h
	dr.mu.Unlock()
	return h, nil
}

func (dr *DataResolver) names() []string {
	var out []string
	for _, c := range dr.conns.List() {
		out = append(out, c.Name)
	}
	return out
}

func driverFor(kind string) string {
	switch kind {
	case "mysql":
		return "mysql"
	case "oracle":
		return "oracle"
	case "sqlserver":
		return "sqlserver"
	default:
		return "sqlite"
	}
}

// Query runs a read-only SELECT against the given FineReport connection name.
func (dr *DataResolver) Query(conn, query string) ([]string, [][]any, error) {
	conn = strings.TrimSpace(conn)
	if conn == "" {
		conn = "FRDemo"
	}
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	if query == "" {
		return nil, nil, fmt.Errorf("SQL 为空")
	}
	head := strings.ToUpper(strings.TrimLeft(query, " \t\r\n("))
	if !strings.HasPrefix(head, "SELECT") && !strings.HasPrefix(head, "WITH") {
		return nil, nil, fmt.Errorf("仅允许 SELECT 查询")
	}
	h, err := dr.handle(conn)
	if err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	rows, err := h.db.Query(query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	out := make([][]any, 0, 64)
	for rows.Next() {
		if len(out) >= 20000 {
			break
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		for i, v := range raw {
			if b, ok := v.([]byte); ok {
				raw[i] = string(b)
			}
		}
		out = append(out, raw)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return cols, out, nil
}

// Dictionary loads a database dictionary table into a kv map.
func (dr *DataResolver) Dictionary(conn, table string, ki, vi int) (map[string]string, error) {
	key := fmt.Sprintf("%s|%s|%d|%d", conn, table, ki, vi)
	dr.mu.Lock()
	if m, ok := dr.dicts[key]; ok {
		dr.mu.Unlock()
		return m, nil
	}
	if e, ok := dr.dictErr[key]; ok {
		dr.mu.Unlock()
		return nil, fmt.Errorf("%s", e)
	}
	dr.mu.Unlock()

	table = strings.TrimSpace(table)
	if table == "" {
		return nil, fmt.Errorf("字典表为空")
	}
	// Table names come from trusted template XML; still escape quotes.
	q := `SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" LIMIT 10000`
	_, rows, err := dr.Query(conn, q)
	m := make(map[string]string, len(rows))
	if err == nil {
		for _, row := range rows {
			if ki < 0 || vi < 0 || ki >= len(row) || vi >= len(row) {
				continue
			}
			k := plainValue(row[ki])
			v := plainValue(row[vi])
			m[k] = v
		}
	}
	dr.mu.Lock()
	if err != nil {
		dr.dictErr[key] = err.Error()
	} else {
		dr.dicts[key] = m
	}
	dr.mu.Unlock()
	return m, err
}

// ParseDatasets builds dataset descriptors from a workbook XML tree.
func ParseDatasets(workbook *XNode) map[string]*Dataset {
	out := make(map[string]*Dataset)
	if workbook == nil {
		return out
	}
	tdm := workbook.Child("TableDataMap")
	if tdm == nil {
		return out
	}
	for _, td := range tdm.ChildAll("TableData") {
		ds := &Dataset{
			Name:      td.Attr("name"),
			ClassName: td.Attr("class"),
		}
		if ds.Name == "" {
			continue
		}
		switch {
		case strings.Contains(ds.ClassName, "DBTableData"):
			if c := td.Find("Connection"); c != nil {
				if n := c.Find("DatabaseName"); n != nil {
					ds.Conn = n.TextTrim()
				}
			}
			if q := td.Child("Query"); q != nil {
				ds.Query = q.TextTrim()
			}
		case strings.Contains(ds.ClassName, "EmbeddedTableData"):
			if n := td.Child("ColumnNames"); n != nil {
				ds.Columns = EmbeddedColumns(n.TextTrim())
			}
			if t := td.Child("ColumnTypes"); t != nil {
				ds.ColumnTypes = splitTypes(t.TextTrim())
			}
			for _, rd := range td.ChildAll("RowData") {
				types := ds.ColumnTypes
				if at := rd.Attr("ColumnTypes"); at != "" {
					types = splitTypes(at)
				}
				raw, err := DecodeFineString(rd.Text)
				if err != nil {
					if len(raw) == 0 {
						ds.Warnings = append(ds.Warnings, fmt.Sprintf("数据集 %s 有一段内置数据无法解码: %v", ds.Name, err))
						continue
					}
					raw = trimPartialPayload(raw)
					ds.Warnings = append(ds.Warnings, fmt.Sprintf("数据集 %s 的内置数据不完整，已使用可解码部分", ds.Name))
				}
				text := strings.TrimPrefix(string(raw), "\ufeff")
				rows := ParseEmbeddedRows(text, types)
				ds.Rows = append(ds.Rows, rows...)
			}
			if len(ds.Rows) > 0 && len(ds.Columns) == 0 {
				n := len(ds.Rows[0])
				for i := 0; i < n; i++ {
					ds.Columns = append(ds.Columns, fmt.Sprintf("列%d", i+1))
				}
			}
			ds.Loaded = true
		default:
			ds.Err = "暂不支持的数据集类型: " + ds.ClassName
		}
		out[ds.Name] = ds
	}
	return out
}

// trimPartialPayload drops a truncated final line so partially inflated
// embedded data still produces usable rows.
func trimPartialPayload(b []byte) []byte {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[:i+1]
	}
	return b
}

func splitTypes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// Load executes the dataset. Database datasets are re-run when request
// parameters change so cached FVS templates still honour previews.
func (ds *Dataset) Load(dr *DataResolver, params map[string]string) error {
	key := paramsKey(params)
	if ds.Loaded {
		if ds.Query == "" || ds.loadKey == key {
			return nil
		}
	}
	q, err := ExpandSQL(ds.Query, params)
	if err != nil {
		ds.Err = err.Error()
		return err
	}
	cols, rows, err := dr.Query(ds.Conn, q)
	if err != nil {
		ds.Err = err.Error()
		return err
	}
	ds.Columns = cols
	ds.Rows = rows
	ds.Loaded = true
	ds.Err = ""
	ds.loadKey = key
	return nil
}

func paramsKey(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
		b.WriteByte('\x00')
	}
	return b.String()
}

// ColumnIndex returns the index of a dataset column, case-insensitive.
func (ds *Dataset) ColumnIndex(name string) int {
	for i, c := range ds.Columns {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

// ExpandSQL replaces ${...} placeholders in a dataset query. A bare parameter
// name becomes a quoted SQL literal, while any other content is evaluated as a
// FineReport formula and inserted verbatim as a SQL fragment.
func ExpandSQL(query string, params map[string]string) (string, error) {
	if !strings.Contains(query, "${") {
		return query, nil
	}
	var b strings.Builder
	for {
		i := strings.Index(query, "${")
		if i < 0 {
			b.WriteString(query)
			break
		}
		end := sqlPlaceholderEnd(query, i+2)
		if end < 0 {
			b.WriteString(query)
			break
		}
		b.WriteString(query[:i])
		expr := strings.TrimSpace(query[i+2 : end])
		if isPlainIdent(expr) {
			if val, ok := lookupParam(params, expr); ok {
				b.WriteString(sqlLiteral(query, i, end+1, val))
			} else {
				b.WriteString(query[i : end+1])
			}
		} else {
			val, err := evalSQLExpr(expr, params)
			if err != nil {
				return "", fmt.Errorf("SQL 表达式 ${%s} 求值失败: %w", expr, err)
			}
			b.WriteString(val)
		}
		query = query[end+1:]
	}
	return b.String(), nil
}

// sqlPlaceholderEnd finds the closing brace, ignoring braces inside literals.
func sqlPlaceholderEnd(s string, from int) int {
	var quote byte
	for i := from; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(s) && s[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '}':
			return i
		}
	}
	return -1
}

func isPlainIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

func sqlLiteral(query string, start, end int, val string) string {
	quotedBefore := start > 0 && query[start-1] == '\''
	quotedAfter := end < len(query) && query[end] == '\''
	switch {
	case quotedBefore && quotedAfter:
		return strings.ReplaceAll(val, "'", "''")
	case isNumeric(val):
		return val
	case strings.EqualFold(val, "null"):
		return "NULL"
	}
	return "'" + strings.ReplaceAll(val, "'", "''") + "'"
}

func evalSQLExpr(expr string, params map[string]string) (string, error) {
	expr = strings.TrimPrefix(expr, "=")
	ctx := NewEvalContext()
	for k, v := range params {
		// Values spliced into hand-written SQL fragments must not break out of
		// the quotes the template author put around them.
		ctx.Params[k] = strings.ReplaceAll(v, "'", "''")
	}
	ctx.Ident = func(name string) (any, bool) {
		v, ok := lookupParam(params, name)
		if !ok {
			return "", true
		}
		return strings.ReplaceAll(v, "'", "''"), true
	}
	v, err := EvalFormula(expr, ctx)
	if err != nil {
		return "", err
	}
	return stringOf(scalar(v)), nil
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

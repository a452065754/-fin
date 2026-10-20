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
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"
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

// DataResolver executes template datasets against the bundled SQLite database.
type DataResolver struct {
	Webroot string

	mu      sync.Mutex
	db      *sql.DB
	dbPath  string
	dicts   map[string]map[string]string
	dictErr map[string]string
}

func NewDataResolver(webroot string) *DataResolver {
	return &DataResolver{
		Webroot: webroot,
		dicts:   make(map[string]map[string]string),
		dictErr: make(map[string]string),
	}
}

func (dr *DataResolver) Close() {
	if dr.db != nil {
		dr.db.Close()
		dr.db = nil
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
	dr.mu.Lock()
	defer dr.mu.Unlock()
	return dr.ensureDBLocked()
}

func (dr *DataResolver) ensureDBLocked() error {
	if dr.db != nil {
		return nil
	}
	path := dr.frdemoPath()
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return fmt.Errorf("找不到 FRDemo 数据库: %s", path)
	}
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(3000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(2)
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("打开 FRDemo 数据库失败: %w", err)
	}
	dr.db = db
	dr.dbPath = path
	return nil
}

// Query runs a read-only SELECT against the given FineReport connection name.
func (dr *DataResolver) Query(conn, query string) ([]string, [][]any, error) {
	conn = strings.TrimSpace(conn)
	if conn == "" {
		conn = "FRDemo"
	}
	if !strings.EqualFold(conn, "FRDemo") {
		return nil, nil, fmt.Errorf("暂不支持数据连接 %q（当前仅映射 FRDemo）", conn)
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
	dr.mu.Lock()
	err := dr.ensureDBLocked()
	db := dr.db
	dr.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	rows, err := db.Query(query)
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

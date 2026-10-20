package fr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Connection is one FineReport datasource as declared in the embedded HSQLDB
// store (WEB-INF/embed/finedb). Passwords are encrypted by FineReport, so we
// only read the public attributes and take the secret from the environment.
type Connection struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	URL      string `json:"url"`
	Database string `json:"database,omitempty"`
	Username string `json:"username,omitempty"`
	// Kind is one of sqlite / mysql / oracle / sqlserver, derived from the
	// JDBC url so callers never have to reason about driver class names.
	Kind string `json:"kind"`
	// NeedsPassword is false only for file-based engines such as SQLite.
	NeedsPassword bool `json:"needsPassword"`
	// PasswordEnv names the environment variable holding the password.
	PasswordEnv string `json:"passwordEnv,omitempty"`
	// Resolved is true when a password was found in the environment.
	Resolved bool `json:"resolved"`
}

// Connections is the parsed datasource catalogue.
type Connections struct {
	mu    sync.Mutex
	items map[string]*Connection
	order []string
}

const connEntityPrefix = "ConnectionConfig.connections."

// envSafe turns a datasource name into an environment variable fragment.
// Only [A-Za-z0-9_] survive, so Chinese names such as 气密检测77 collapse to
// a stable ASCII token: PWD_<hex of UTF-8 bytes>.
func envSafe(name string) string {
	var b strings.Builder
	ascii := true
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			continue
		}
		ascii = false
		break
	}
	if ascii && b.Len() > 0 {
		return b.String()
	}
	var hex strings.Builder
	for _, c := range []byte(name) {
		fmt.Fprintf(&hex, "%02X", c)
	}
	return hex.String()
}

// PasswordEnvFor returns the environment variable that holds the password for
// the named datasource. Exported so the CLI can print the mapping to the user.
func PasswordEnvFor(name string) string {
	return "FINE_PWD_" + envSafe(name)
}

func jdbcKind(url string) string {
	l := strings.ToLower(url)
	switch {
	case strings.HasPrefix(l, "jdbc:sqlite"):
		return "sqlite"
	case strings.HasPrefix(l, "jdbc:mysql:"):
		return "mysql"
	case strings.HasPrefix(l, "jdbc:oracle:"):
		return "oracle"
	case strings.HasPrefix(l, "jdbc:sqlserver:"):
		return "sqlserver"
	default:
		return ""
	}
}

// hsqlUnescape reverses the SQL string literal escaping used by db.script.
func hsqlUnescape(s string) string {
	return strings.ReplaceAll(s, "''", "'")
}

// decodeUnicodeEscapes turns the \uXXXX sequences FineReport uses for non-ASCII
// keys (datasource names such as 气密检测77) back into real runes, so lookups
// by the decoded name work.
func decodeUnicodeEscapes(s string) string {
	if !strings.Contains(s, `\u`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+5 < len(s) && s[i+1] == 'u' {
			if v, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
				b.WriteRune(rune(v))
				i += 6
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// LoadConnections reads WEB-INF/embed/finedb/db.script and returns every
// datasource configured in this FineReport installation.
func LoadConnections(webroot string) (*Connections, error) {
	script := filepath.Join(webroot, "WEB-INF", "embed", "finedb", "db.script")
	f, err := os.Open(script)
	if err != nil {
		return nil, fmt.Errorf("读取连接配置失败: %w", err)
	}
	defer f.Close()

	cs := &Connections{items: map[string]*Connection{}}

	// Raw key -> value for every ConnectionConfig.connections.* row.
	raw := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		// db.script literals look like VALUES('key','value'); the separator
		// between key and value is three characters: quote comma quote.
		idx := strings.Index(line, "INSERT INTO FINE_CONF_ENTITY VALUES('")
		if idx < 0 {
			continue
		}
		rest := line[idx+len("INSERT INTO FINE_CONF_ENTITY VALUES('"):]
		q := strings.Index(rest, "','")
		if q < 0 {
			continue
		}
		key := decodeUnicodeEscapes(hsqlUnescape(rest[:q]))
		val := hsqlUnescape(strings.TrimSuffix(rest[q+3:], "')"))
		raw[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("连接配置为空或格式不识别: %s", script)
	}

	// The declared order lives in the comma-joined JSON array.
	if list, ok := raw["ConnectionConfig.connections"]; ok {
		var names []string
		if err := json.Unmarshal([]byte(list), &names); err == nil {
			cs.order = names
		}
	}
	if len(cs.order) == 0 {
		cs.order = sortedKeys(raw)
	}

	for _, name := range cs.order {
		base := connEntityPrefix + name + "."
		url := strings.TrimSpace(raw[base+"url"])
		if url == "" {
			continue
		}
		kind := jdbcKind(url)
		if kind == "" {
			continue
		}
		c := &Connection{
			Name:     name,
			Driver:   raw[base+"driver"],
			URL:      url,
			Database: emptyToEmpty(raw[base+"database"]),
			Username: emptyToEmpty(raw[base+"authentication.username"]),
			Kind:     kind,
		}
		c.NeedsPassword = kind != "sqlite"
		if c.NeedsPassword {
			c.PasswordEnv = PasswordEnvFor(name)
			c.Resolved = os.Getenv(c.PasswordEnv) != ""
		}
		cs.items[name] = c
	}
	if len(cs.items) == 0 {
		return nil, fmt.Errorf("未解析到任何可用数据连接: %s", script)
	}
	return cs, nil
}

func sortedKeys(raw map[string]string) []string {
	var out []string
	for k := range raw {
		if !strings.HasPrefix(k, connEntityPrefix) {
			continue
		}
		rest := k[len(connEntityPrefix):]
		if strings.Count(rest, ".") != 1 {
			continue
		}
		out = append(out, rest[:strings.Index(rest, ".")])
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func emptyToEmpty(s string) string {
	if s == "__EMPTY__" {
		return ""
	}
	return s
}

// List returns every connection in declaration order.
func (c *Connections) List() []*Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*Connection, 0, len(c.order))
	for _, n := range c.order {
		if it := c.items[n]; it != nil {
			out = append(out, it)
		}
	}
	return out
}

// Get returns a connection by exact name; lookup is case-insensitive because
// template files reference datasources with inconsistent casing.
func (c *Connections) Get(name string) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	if it := c.items[name]; it != nil {
		return it
	}
	for k, v := range c.items {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// ResolveSQLitePath maps a FineReport SQLite url onto a real file. The url uses
// ${ENV_HOME} which FineReport expands at runtime; we resolve it against the
// webroot the same way the bundled FRDemo.db lives under <webroot>/help.
func ResolveSQLitePath(webroot, jdbcURL string) string {
	rest := jdbcURL
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	rest = strings.TrimPrefix(rest, "${ENV_HOME}/../")
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return ""
	}
	cand := filepath.Join(webroot, filepath.FromSlash(rest))
	if st, err := os.Stat(cand); err == nil && !st.IsDir() {
		return cand
	}
	alt := filepath.Join(webroot, "WEB-INF", filepath.FromSlash(rest))
	if st, err := os.Stat(alt); err == nil && !st.IsDir() {
		return alt
	}
	return cand
}

// BuildDSN converts a FineReport connection plus its password into a driver
// specific data source name for database/sql.
func BuildDSN(c *Connection, password, webroot string) (string, error) {
	switch c.Kind {
	case "sqlite":
		p := ResolveSQLitePath(webroot, c.URL)
		if p == "" {
			return "", fmt.Errorf("无法定位 SQLite 文件: %s", c.URL)
		}
		return "file:" + filepath.ToSlash(p) + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(3000)", nil
	case "mysql":
		host, port, db := parseMySQL(c.URL)
		if host == "" {
			return "", fmt.Errorf("无法解析 MySQL URL: %s", c.URL)
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=false&timeout=10s&readTimeout=60s", c.Username, password, host, port, db), nil
	case "oracle":
		dsn, err := oracleDSN(c, password)
		if err != nil {
			return "", err
		}
		return dsn, nil
	case "sqlserver":
		q := sqlserverParams(c.URL)
		q.Set("database", sqlserverDatabase(c.URL))
		if password != "" {
			q.Set("password", password)
		}
		if c.Username != "" {
			q.Set("user id", c.Username)
		}
		q.Set("encrypt", "disable")
		q.Set("connection timeout", "15")
		return q.Encode(), nil
	default:
		return "", fmt.Errorf("不支持的数据源类型: %s", c.Kind)
	}
}

func parseMySQL(u string) (host, port, db string) {
	rest := strings.TrimPrefix(u, "jdbc:mysql://")
	if i := strings.Index(rest, "/"); i >= 0 {
		db = rest[i+1:]
		rest = rest[:i]
	}
	// Strip any query string such as ?useSSL=false
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	port = "3306"
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		port = rest[i+1:]
		rest = rest[:i]
	}
	return rest, port, db
}

func sqlserverDatabase(u string) string {
	lower := strings.ToLower(u)
	i := strings.Index(lower, "databasename=")
	if i < 0 {
		return ""
	}
	rest := u[i+len("databasename="):]
	if j := strings.IndexAny(rest, ";"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

func sqlserverParams(u string) url.Values {
	out := url.Values{}
	rest := strings.TrimPrefix(u, "jdbc:sqlserver://")
	for _, seg := range strings.Split(rest, ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(kv[0]))
		v := strings.TrimSpace(kv[1])
		switch k {
		case "databasename":
			continue
		case "port":
			out["port"] = []string{v}
		default:
			if k == "encrypt" {
				continue
			}
			out[k] = []string{v}
		}
	}
	return out
}

// oracleDSN translates jdbc:oracle:thin:@host:port:sid into go-ora's URL form.
// The old colon-SID syntax and the //host/service syntax are both accepted by
// Oracle, so go-ora understands them unchanged.
func oracleDSN(c *Connection, password string) (string, error) {
	u := c.URL
	if !strings.HasPrefix(strings.ToLower(u), "jdbc:oracle:thin:") {
		return "", fmt.Errorf("暂不支持的 Oracle URL 形式: %s", u)
	}
	body := strings.TrimPrefix(u, "jdbc:oracle:thin:@")
	if body == "" {
		return "", fmt.Errorf("Oracle URL 缺少主机: %s", u)
	}
	// Oracle identifiers cannot contain these characters unescaped.
	q := func(s string) string {
		s = strings.ReplaceAll(s, "/", "%2F")
		s = strings.ReplaceAll(s, ":", "%3A")
		s = strings.ReplaceAll(s, "?", "%3F")
		s = strings.ReplaceAll(s, "#", "%23")
		s = strings.ReplaceAll(s, "[", "%5B")
		s = strings.ReplaceAll(s, "]", "%5D")
		return s
	}
	user := q(c.Username)
	host := body
	if strings.HasPrefix(host, "//") {
		// TNS alias: leave untouched.
		return fmt.Sprintf("oracle://%s:%s@%s", user, q(password), host), nil
	}
	return fmt.Sprintf("oracle://%s:%s@%s", user, q(password), q(host)), nil
}

// Password returns the password for a connection from the environment.
func (c *Connection) Password() string { return os.Getenv(c.PasswordEnv) }

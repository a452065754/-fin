package fr

import (
	"os"
	"strings"
	"testing"
)

const testWebroot = `F:/tomcat-win64/webapps/webroot`

func TestLoadConnections(t *testing.T) {
	cs, err := LoadConnections(testWebroot)
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	list := cs.List()
	t.Logf("共 %d 个数据连接", len(list))
	for _, c := range list {
		t.Logf("%-20s kind=%-9s user=%-8s %s", c.Name, c.Kind, c.Username, c.URL)
	}
	if len(list) == 0 {
		t.Fatal("未解析到任何数据连接")
	}

	byName := map[string]*Connection{}
	for _, c := range list {
		byName[c.Name] = c
	}
	// The bundled demo database must always be present and password-free.
	frdemo := cs.Get("FRDemo")
	if frdemo == nil {
		t.Fatal("缺少 FRDemo 连接")
	}
	if frdemo.Kind != "sqlite" || frdemo.NeedsPassword {
		t.Errorf("FRDemo 应为免密 sqlite, got kind=%s needsPwd=%v", frdemo.Kind, frdemo.NeedsPassword)
	}
	// Case-insensitive lookup, because templates vary in casing.
	if cs.Get("frdemo") == nil {
		t.Error("连接名查找应大小写不敏感")
	}
	for _, c := range list {
		if c.Kind == "" {
			t.Errorf("连接 %s 未识别出类型: %s", c.Name, c.URL)
		}
	}
}

func TestResolveSQLitePath(t *testing.T) {
	p := ResolveSQLitePath(testWebroot, "jdbc:sqlite://${ENV_HOME}/../help/FRDemo.db")
	t.Logf("resolved=%s", p)
	if !strings.HasSuffix(strings.ToLower(p), "frdemo.db") {
		t.Errorf("路径未指向 FRDemo.db: %s", p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("解析出的路径不存在: %v", err)
	}
}

func TestBuildDSN(t *testing.T) {
	cases := []struct {
		kind string
		conn *Connection
		want string
	}{
		{
			"mysql",
			&Connection{Kind: "mysql", Username: "root", URL: "jdbc:mysql://192.168.0.27:3306/27jk"},
			"root:pw@tcp(192.168.0.27:3306)/27jk?parseTime=false&timeout=10s&readTimeout=60s",
		},
		{
			"oracle",
			&Connection{Kind: "oracle", Username: "XYDS", URL: "jdbc:oracle:thin:@192.168.0.28:1521:orcl"},
			"oracle://XYDS:pw@192.168.0.28%3A1521%3Aorcl",
		},
		{
			"sqlserver",
			&Connection{Kind: "sqlserver", Username: "sa", URL: "jdbc:sqlserver://192.168.0.19:1433;databaseName=OAData;encrypt = false"},
			"connection+timeout=15&database=OAData&encrypt=disable&password=pw&user+id=sa",
		},
	}
	for _, tc := range cases {
		got, err := BuildDSN(tc.conn, "pw", testWebroot)
		if err != nil {
			t.Fatalf("%s: %v", tc.kind, err)
		}
		if got != tc.want {
			t.Errorf("%s DSN 不符\n got=%s\nwant=%s", tc.kind, got, tc.want)
		}
	}
}

func TestPasswordEnvFor(t *testing.T) {
	// Pure ASCII names stay readable.
	if got := PasswordEnvFor("MESCS"); got != "FINE_PWD_MESCS" {
		t.Errorf("got %s", got)
	}
	// Non-ASCII names must still yield a usable env var name.
	got := PasswordEnvFor("气密检测77")
	if !strings.HasPrefix(got, "FINE_PWD_") {
		t.Errorf("前缀错误: %s", got)
	}
	for _, r := range got {
		if r > 127 {
			t.Errorf("环境变量名含非 ASCII 字符: %s", got)
		}
	}
	// Distinct Chinese names must not collide.
	if PasswordEnvFor("气密检测77") == PasswordEnvFor("气密检测36") {
		t.Error("不同连接名映射到相同环境变量")
	}
}

// A missing password must produce an actionable message, never a panic.
func TestUnknownConnection(t *testing.T) {
	dr := NewDataResolver(testWebroot)
	defer dr.Close()
	if _, _, err := dr.Query("不存在的连接", "SELECT 1"); err == nil {
		t.Error("未知连接应报错")
	} else if !strings.Contains(err.Error(), "数据连接") {
		t.Errorf("错误信息不清晰: %v", err)
	}
}

func TestReadOnlyGuard(t *testing.T) {
	dr := NewDataResolver(testWebroot)
	defer dr.Close()
	for _, q := range []string{"DELETE FROM x", "UPDATE x SET a=1", "DROP TABLE x", "  ; DROP TABLE x"} {
		if _, _, err := dr.Query("FRDemo", q); err == nil {
			t.Errorf("非 SELECT 语句未被拒绝: %s", q)
		}
	}
}

// FRDemo must keep working after the multi-connection refactor.
func TestFRDemoStillQueryable(t *testing.T) {
	dr := NewDataResolver(testWebroot)
	defer dr.Close()
	cols, rows, err := dr.Query("FRDemo", "SELECT 1 AS one")
	if err != nil {
		t.Fatalf("FRDemo 查询失败: %v", err)
	}
	if len(cols) != 1 || len(rows) != 1 {
		t.Fatalf("结果异常: cols=%v rows=%v", cols, rows)
	}
}

// A connection whose password is missing must fail with the env var name,
// never panic and never fall back to another datasource.
func TestMissingPasswordIsActionable(t *testing.T) {
	dr := NewDataResolver(testWebroot)
	defer dr.Close()
	os.Unsetenv(PasswordEnvFor("MESCS"))
	_, _, err := dr.Query("MESCS", "SELECT 1 FROM dual")
	if err == nil {
		t.Skip("MESCS 密码已在环境中配置，跳过")
	}
	msg := err.Error()
	if !strings.Contains(msg, "FINE_PWD_MESCS") {
		t.Errorf("错误信息未给出环境变量名: %v", err)
	}
	if !strings.Contains(msg, "MESCS") {
		t.Errorf("错误信息未指出是哪个连接: %v", err)
	}
}

// Every declared connection must resolve to a supported driver.
func TestAllConnectionsHaveDriver(t *testing.T) {
	cs, err := LoadConnections(testWebroot)
	if err != nil {
		t.Skipf("webroot 不可用，跳过: %v", err)
	}
	for _, c := range cs.List() {
		if driverFor(c.Kind) == "" {
			t.Errorf("连接 %s 的类型 %s 无对应驱动", c.Name, c.Kind)
		}
		if c.Kind == "sqlite" {
			// SQLite needs no password but must still resolve to a real file.
			p := ResolveSQLitePath(testWebroot, c.URL)
			if _, err := os.Stat(p); err != nil {
				t.Errorf("sqlite 连接 %s 文件不存在: %s", c.Name, p)
			}
		}
		// Every connection must produce a DSN without error.
		if _, err := BuildDSN(c, "", testWebroot); err != nil {
			t.Errorf("连接 %s 生成 DSN 失败: %v", c.Name, err)
		}
	}
}

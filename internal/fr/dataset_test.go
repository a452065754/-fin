package fr

import (
	"strings"
	"testing"
)

func TestExpandSQL(t *testing.T) {
	q := `SELECT * FROM 销售明细表 WHERE 1=1 ${if(len(name) == 0,"","and 店铺名称 = '" + name + "'")} order by 销售时间`

	got, err := ExpandSQL(q, nil)
	if err != nil {
		t.Fatalf("expand without params: %v", err)
	}
	if strings.Contains(got, "${") || !strings.Contains(got, "WHERE 1=1  order by") {
		t.Fatalf("unexpected empty-param SQL: %s", got)
	}

	got, err = ExpandSQL(q, map[string]string{"name": "华北"})
	if err != nil {
		t.Fatalf("expand with params: %v", err)
	}
	if !strings.Contains(got, "店铺名称 = '华北'") {
		t.Fatalf("parameter was not injected: %s", got)
	}

	got, err = ExpandSQL(`SELECT * FROM t WHERE name = '${name}'`, map[string]string{"name": "O'Brien"})
	if err != nil {
		t.Fatalf("expand quoted param: %v", err)
	}
	if got != `SELECT * FROM t WHERE name = 'O''Brien'` {
		t.Fatalf("unexpected quoted SQL: %s", got)
	}
}

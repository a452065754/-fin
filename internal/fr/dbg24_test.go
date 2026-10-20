package fr

import "testing"

func TestDbgBox(t *testing.T) {
	tpl, _ := ParseCPTFile(webroot + `/WEB-INF/reportlets/doc/Chart/BoxPlot/分组箱型图.cpt`)
	for n, d := range tpl.Datasets {
		t.Logf("ds=%q rows=%d cols=%v warn=%v", n, len(d.Rows), d.Columns, d.Warnings)
		for i, r := range d.Rows {
			t.Logf("  row%d %v", i, r)
			if i >= 5 {
				break
			}
		}
	}
}

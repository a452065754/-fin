package fr

import (
	"os"
	"path/filepath"
	"testing"
)

// webroot points at the FineReport installation the tests read templates
// from. Override it with the FR_TEST_WEBROOT environment variable; the tests
// skip themselves when no installation is available, so CI can run without
// FineReport present.
var webroot = resolveTestWebroot()

const defaultTestWebroot = `F:/tomcat-win64/webapps/webroot`

func resolveTestWebroot() string {
	if v := os.Getenv("FR_TEST_WEBROOT"); v != "" {
		return v
	}
	return defaultTestWebroot
}

// requireWebroot skips the test when the FineReport installation is missing.
func requireWebroot(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(webroot, "WEB-INF", "reportlets")); err != nil {
		t.Skipf("未找到 FineReport 模板目录（%s），跳过。需要时用 FR_TEST_WEBROOT 指定", webroot)
	}
}

func renderDemo(t *testing.T, path string) *Preview {
	t.Helper()
	tpl, err := ParseCPTFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)
	if p.Error != "" {
		t.Fatalf("render: %s", p.Error)
	}
	return p
}

func TestGroupReport(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/demo/basic/分组统计.cpt`)
	sh := p.Sheets[0]
	t.Logf("rows=%d cols=%d cells=%d warnings=%v", len(sh.Rows), len(sh.Cols), len(sh.Cells), p.Warnings)
	// The first supplier must merge 5 rows: 2 details + 2 subtotals + 1 total.
	found := false
	for _, c := range sh.Cells {
		if c.R == 5 && c.C == 0 && c.RS == 5 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected supplier cell rowspan 5 at (5,0)")
	}
	if len(sh.Rows) < 20 {
		t.Errorf("unexpected row count %d", len(sh.Rows))
	}
}

func TestHorizontalReport(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/demo/basic/列分栏.cpt`)
	sh := p.Sheets[0]
	t.Logf("rows=%d cols=%d cells=%d width=%.0f warnings=%v", len(sh.Rows), len(sh.Cols), len(sh.Cells), sh.Width, p.Warnings)
	// The demo dataset has 85 rows: one label column plus 85 expanded columns.
	if len(sh.Cols) != 86 {
		t.Errorf("expected 86 columns, got %d", len(sh.Cols))
	}
	var seq int
	for _, c := range sh.Cells {
		if c.R == 1 && c.C == 1 {
			seq = 1
		}
	}
	if seq == 0 {
		t.Errorf("missing seq header cell at (1,1)")
	}
}

func TestColumnChart(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/doc/Chart/ColumnChart/圆角属性和柱子宽度自定义柱形图.cpt`)
	if len(p.Sheets) == 0 {
		t.Fatal("no sheet")
	}
	var chart *ChartSpec
	for _, c := range p.Sheets[0].Cells {
		if c.Chart != nil {
			chart = c.Chart
			break
		}
	}
	if chart == nil {
		t.Fatal("no chart rendered")
	}
	t.Logf("kind=%s title=%q cats=%d series=%d colors=%d legend=%v pos=%q warnings=%v",
		chart.Kind, chart.Title, len(chart.Categories), len(chart.Series), len(chart.Colors), chart.Legend, chart.LegendPos, p.Warnings)
	if chart.Kind != "bar" || len(chart.Series) == 0 {
		t.Errorf("unexpected chart %+v", chart)
	}
}

func TestFVSInsurance(t *testing.T) {
	requireWebroot(t)
	tpl, err := ParseFVSFile(webroot + `/WEB-INF/reportlets/demo/analytics/保险数据大屏.fvs`)
	if err != nil {
		t.Fatalf("parse fvs: %v", err)
	}
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)
	if len(p.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(p.Pages))
	}
	page := p.Pages[0]
	var charts, sheets int
	for _, w := range page.Widgets {
		if w.Chart != nil {
			charts++
		}
		if w.Sheet != nil {
			sheets++
		}
	}
	t.Logf("page=%q size=%.0fx%.0f widgets=%d charts=%d sheets=%d warnings=%v",
		page.Name, page.Width, page.Height, len(page.Widgets), charts, sheets, p.Warnings)
	if charts == 0 {
		t.Errorf("expected charts in insurance dashboard")
	}
	if sheets == 0 {
		t.Errorf("expected report blocks in insurance dashboard")
	}
	if page.BackgroundImage == "" {
		t.Errorf("expected image background")
	}
}

func TestFVSTabs(t *testing.T) {
	requireWebroot(t)
	tpl, err := ParseFVSFile(webroot + `/WEB-INF/reportlets/demo/analytics/XX证券公司年度计划表.fvs`)
	if err != nil {
		t.Fatalf("parse fvs: %v", err)
	}
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)
	var tab *TabInfo
	var hidden int
	for i := range p.Pages[0].Widgets {
		w := &p.Pages[0].Widgets[i]
		if w.Tab != nil {
			tab = w.Tab
		}
		if w.Hidden {
			hidden++
		}
	}
	if tab == nil || len(tab.Tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %+v", tab)
	}
	t.Logf("tabs=%v active=%d hidden=%d warnings=%v", tab.Tabs, tab.Active, hidden, p.Warnings)
	if hidden == 0 {
		t.Errorf("expected inactive pane widgets to be hidden")
	}
}

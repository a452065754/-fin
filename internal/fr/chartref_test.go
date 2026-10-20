package fr

import (
	"strings"
	"testing"
)

// A dataset may declare its own parameters with defaults. Those defaults must
// be used when the request does not supply them, otherwise the query runs
// with an empty placeholder and the dataset fails to load.
func TestDatasetOwnParameters(t *testing.T) {
	requireWebroot(t)
	tpl, err := ParseCPTFile(webroot + `/WEB-INF/reportlets/GettingStarted.cpt`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ds := tpl.Datasets["ds1"]
	if ds == nil {
		t.Fatal("缺少 ds1")
	}
	if len(ds.Params) == 0 {
		t.Fatal("ds1 的自身参数未被解析")
	}
	t.Logf("ds1 参数: %+v", ds.Params)
	found := false
	for _, p := range ds.Params {
		if p.Name == "地区" && p.Default != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("未解析到「地区」参数的默认值: %+v", ds.Params)
	}
	// The request parameter must win over the dataset default.
	merged := ds.effectiveParams(map[string]string{"地区": "华北"})
	if merged["地区"] != "华北" {
		t.Errorf("请求参数未覆盖数据集默认值: %v", merged["地区"])
	}
	// Without a request parameter the default must be used.
	merged = ds.effectiveParams(nil)
	if merged["地区"] == "" {
		t.Error("数据集默认参数未生效")
	}
}

// A template whose dataset has its own parameters must still load, and its
// report cells must carry real values.
func TestTemplateWithDatasetParamsRenders(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/GettingStarted.cpt`)
	for _, w := range p.Warnings {
		if strings.Contains(w, "数据集") {
			t.Errorf("数据集加载告警: %s", w)
		}
	}
	// At least one cell must carry a numeric value.
	var numbers int
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if _, ok := toFloat(c.Text); ok {
				numbers++
			}
		}
	}
	t.Logf("含数值的单元格: %d", numbers)
	if numbers == 0 {
		t.Error("没有任何单元格带数值，说明数据集参数未生效")
	}
}

// A chart whose data definition references report cells (=C3, =C4, =B4)
// must be resolved from the rendered grid.
func TestChartFromReportCells(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/GettingStarted.cpt`)
	var chart *ChartSpec
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Chart != nil {
				chart = c.Chart
			}
		}
	}
	if chart == nil {
		t.Fatal("模板中没有图表")
	}
	t.Logf("kind=%s cats=%v series=%d", chart.Kind, chart.Categories, len(chart.Series))
	if len(chart.Series) == 0 {
		t.Fatal("引用单元格的图表没有数据系列")
	}
	total := 0
	for _, s := range chart.Series {
		t.Logf("  系列 %q -> %v", s.Name, s.Data)
		total += len(s.Data)
	}
	if total == 0 {
		t.Fatal("图表系列没有数据点")
	}
	if len(chart.Categories) == 0 {
		t.Error("图表没有分类标签")
	}
}

// The cell reference parser must handle the shapes FineReport writes.
func TestNormalizeCellRef(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"=C3", "C3"},
		{"C3", "C3"},
		{"=$B$4", "B4"},
		{"=b4", "B4"},
		{" = C3 ", "C3"},
		{"=AA12", "AA12"},
		{"", ""},
		{"=sum(C4)", ""},
		{"=C3+C4", ""},
	}
	for _, tc := range cases {
		got := normalizeCellRef(tc.in)
		if got != tc.want {
			t.Errorf("normalizeCellRef(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}
}

// A range reference falls back to its first cell.
func TestNormalizeCellRefRange(t *testing.T) {
	if got := normalizeCellRef("=B2:B9"); got != "B2" {
		t.Errorf("区间应取首格, 得到 %q", got)
	}
}

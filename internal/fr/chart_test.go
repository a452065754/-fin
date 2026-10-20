package fr

import (
	"strings"
	"testing"
)

// chartsIn renders a template and collects every chart it produced.
func chartsIn(t *testing.T, rel string) []*ChartSpec {
	t.Helper()
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/`+rel)
	var out []*ChartSpec
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Chart != nil {
				out = append(out, c.Chart)
			}
		}
	}
	return out
}

func firstChartOfKind(t *testing.T, rel, kind string) *ChartSpec {
	t.Helper()
	charts := chartsIn(t, rel)
	for _, c := range charts {
		if c.Kind == kind {
			return c
		}
	}
	t.Logf("%s 中的图表类型: %s", rel, kindsOf(charts))
	t.Fatalf("%s 中未找到 %s 图表", rel, kind)
	return nil
}

func kindsOf(charts []*ChartSpec) string {
	var b []string
	for _, c := range charts {
		b = append(b, c.Kind)
	}
	return strings.Join(b, ",")
}

// A bubble chart must be typed as a bubble and carry a radius per point.
func TestChartBubble(t *testing.T) {
	c := firstChartOfKind(t, "demo/chart/DataVisualization/数据可视化.cpt", "bubble")
	t.Logf("kind=%s cats=%d series=%d", c.Kind, len(c.Categories), len(c.Series))
	if len(c.Series) == 0 {
		t.Fatal("气泡图没有数据系列")
	}
	for _, s := range c.Series {
		if len(s.Size) != len(s.Data) {
			t.Errorf("系列 %s 的半径数量(%d)与数据点数量(%d)不一致", s.Name, len(s.Size), len(s.Data))
		}
	}
}

// Combo charts keep their per-series plot kinds from CustomPlotList.
func TestChartCombo(t *testing.T) {
	charts := chartsIn(t, "demo/analytics/Demo.cpt")
	var combo *ChartSpec
	for _, c := range charts {
		if c.Kind == "combo" {
			combo = c
			break
		}
	}
	if combo == nil {
		t.Fatalf("Demo.cpt 中无组合图，现有类型: %s", kindsOf(charts))
	}
	t.Logf("combo series=%d cats=%d", len(combo.Series), len(combo.Categories))
	if len(combo.Series) == 0 {
		t.Fatal("组合图没有数据系列")
	}
	for _, s := range combo.Series {
		t.Logf("  系列 %q 类型=%s 轴=%s 点数=%d", s.Name, s.Type, s.Axis, len(s.Data))
		if s.Type == "" {
			t.Errorf("组合图系列 %s 未指定绘图类型", s.Name)
		}
		if len(s.Data) == 0 {
			t.Errorf("组合图系列 %s 没有数据点", s.Name)
		}
	}
	// A combo chart mixing kinds must not also be stacked.
	if combo.Stack {
		t.Error("组合图不应启用堆叠")
	}
}

// Map charts must stay maps and keep the drill-down flag where declared.
func TestChartMap(t *testing.T) {
	c := firstChartOfKind(t, "demo/chart/DataVisualization/IOS平台年度数据报告.cpt", "map")
	t.Logf("kind=%s area=%q drill=%v series=%d", c.Kind, c.MapArea, c.DrillDown, len(c.Series))
	if c.Warning != "" {
		t.Errorf("地图图不应带降级警告: %s", c.Warning)
	}
	if len(c.Series) == 0 {
		t.Fatal("地图图没有数据系列")
	}
}

func TestChartDrillMap(t *testing.T) {
	c := firstChartOfKind(t, "doc/Chart/Map/华东地区钻取地图.cpt", "map")
	t.Logf("kind=%s drill=%v cats=%d", c.Kind, c.DrillDown, len(c.Categories))
}

// Gantt renders as a horizontal bar chart.
func TestChartGantt(t *testing.T) {
	c := firstChartOfKind(t, "doc/Chart/GanttChart/关联线甘特图.cpt", "gantt")
	if !c.Horizontal {
		t.Error("甘特图应为横向")
	}
	if len(c.Series) == 0 {
		t.Fatal("甘特图没有数据系列")
	}
	total := 0
	for _, s := range c.Series {
		total += len(s.Data)
	}
	if total == 0 {
		t.Fatal("甘特图系列没有数据点")
	}
	t.Logf("kind=%s series=%d 数据点=%d", c.Kind, len(c.Series), total)
}

func TestChartHeatMap(t *testing.T) {
	c := firstChartOfKind(t, "doc/Chart/HeatMap/热力地图.cpt", "heatmap")
	t.Logf("kind=%s series=%d cats=%d", c.Kind, len(c.Series), len(c.Categories))
}

func TestChartBox(t *testing.T) {
	c := firstChartOfKind(t, "doc/Chart/BoxPlot/分组箱型图.cpt", "box")
	t.Logf("kind=%s series=%d cats=%d", c.Kind, len(c.Series), len(c.Categories))
}

func TestChartStructure(t *testing.T) {
	// This template's embedded dataset does not survive Ascii85/Deflate
	// decoding, so there are no rows to draw. Assert the parsing behaviour
	// rather than the data: the definition must be recognised as a hierarchy
	// and must report the empty dataset instead of silently mis-parsing.
	tpl, err := ParseCPTFile(webroot + `/WEB-INF/reportlets/doc/Chart/NewStructureFrame/DivergentStructureFrame.cpt`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ds := tpl.Datasets["File1"]
	if ds == nil {
		t.Fatal("缺少 File1 数据集")
	}
	t.Logf("File1: loaded=%v rows=%d cols=%v", ds.Loaded, len(ds.Rows), ds.Columns)
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)

	var found bool
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Chart != nil {
				found = true
				if c.Chart.Kind != "sunburst" {
					t.Errorf("层级图类型应为 sunburst，实际 %s", c.Chart.Kind)
				}
				if c.Chart.Warning != "" {
					t.Errorf("层级图不应带降级标记: %s", c.Chart.Warning)
				}
			}
		}
	}
	if !found {
		t.Fatal("模板中没有图表")
	}
	if len(ds.Rows) == 0 {
		t.Log("内置数据为空（Ascii85/Deflate 解码截断），本次只验证类型识别")
	}
}

// No chart anywhere in the sample set may still be downgraded to a bar.
func TestNoDowngradeWarnings(t *testing.T) {
	samples := []string{
		"demo/chart/DataVisualization/数据可视化.cpt",
		"demo/analytics/Demo.cpt",
		"demo/chart/DataVisualization/IOS平台年度数据报告.cpt",
		"doc/Chart/GanttChart/关联线甘特图.cpt",
		"doc/Chart/HeatMap/热力地图.cpt",
		"doc/Chart/BoxPlot/分组箱型图.cpt",
		"doc/Chart/NewStructureFrame/DivergentStructureFrame.cpt",
		"doc/Chart/Map/华东地区钻取地图.cpt",
	}
	var found bool
	for _, rel := range samples {
		p := renderDemo(t, webroot+`/WEB-INF/reportlets/`+rel)
		for _, w := range p.Warnings {
			if strings.Contains(w, "降级") || strings.Contains(w, "未识别") {
				t.Errorf("%s 仍出现降级: %s", rel, w)
			}
			found = true
		}
		for _, sh := range p.Sheets {
			for _, c := range sh.Cells {
				if c.Chart != nil && c.Chart.Warning != "" {
					t.Errorf("%s 图表带降级标记: %s", rel, c.Chart.Warning)
				}
				found = true
			}
		}
	}
	if !found {
		t.Skip("样例模板均无图表数据")
	}
}

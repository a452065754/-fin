package fr

import (
	"math"
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

// A box plot must be recognised as a box plot, even when its dataset cannot
// be decoded.
func TestChartBoxType(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/doc/Chart/BoxPlot/分组箱型图.cpt`)
	var found bool
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Chart == nil {
				continue
			}
			found = true
			if c.Chart.Kind != "box" {
				t.Errorf("箱型图类型应为 box，实际 %s", c.Chart.Kind)
			}
			if c.Chart.Warning != "" {
				t.Errorf("箱型图不应带降级标记: %s", c.Chart.Warning)
			}
			t.Logf("箱型图: kind=%s 分类=%v 系列=%d", c.Chart.Kind, c.Chart.Categories, len(c.Chart.Series))
		}
	}
	if !found {
		t.Fatal("模板中没有图表")
	}
}

func TestChartStructure(t *testing.T) {
	requireWebroot(t)
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

// A gauge chart must read its value from the meter data definition and know
// its scale, so the browser can draw a real dial instead of an empty frame.
func TestChartGauge(t *testing.T) {
	c := firstChartOfKind(t, "demo/chart/basic/百分比环形仪表盘.cpt", "gauge")
	t.Logf("kind=%s cats=%d series=%d 范围=[%v, %v] auto=%v bands=%d",
		c.Kind, len(c.Categories), len(c.Series), c.GaugeMin, c.GaugeMax, c.GaugeAuto, len(c.GaugeBands))
	if len(c.Series) == 0 {
		t.Fatal("仪表盘没有数据系列")
	}
	total := 0
	for _, s := range c.Series {
		total += len(s.Data)
	}
	if total == 0 {
		t.Fatal("仪表盘系列没有数据点")
	}
	if c.GaugeMax <= c.GaugeMin {
		t.Errorf("仪表盘范围无效: [%v, %v]", c.GaugeMin, c.GaugeMax)
	}
	if len(c.GaugeBands) == 0 {
		t.Error("仪表盘没有解析出阈值分段")
	}
	for _, b := range c.GaugeBands {
		t.Logf("  段 [%v, %v] 颜色=%s", b.Min, b.Max, b.Color)
		if b.Max <= b.Min {
			t.Errorf("阈值分段范围无效: [%v, %v]", b.Min, b.Max)
		}
	}
}

// Several gauges in one template must all produce values.
//
// Two of the five gauges in this template point at a dataset whose embedded
// payload is corrupt: FineReport's own Ascii85InputStream + Inflater also
// fail on it (ZipException: invalid distance code), so the data cannot be
// recovered by any decoder. Verified against the Java oracle in
// tools/oracle/Ascii85Oracle.java — our Go decoder is byte-identical to
// FineReport's for all 718 payloads in the template set.
//
// So the assertion is: every gauge whose dataset decoded must have a value,
// and the corrupt ones must be reported as warnings rather than silently
// producing a wrong number.
func TestChartMultipleGauges(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/demo/chart/basic/多个仪表盘.cpt`)
	var gauges, withData int
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Chart == nil || c.Chart.Kind != "gauge" {
				continue
			}
			gauges++
			if len(c.Chart.Series) > 0 && len(c.Chart.Series[0].Data) > 0 {
				withData++
				t.Logf("仪表盘 %d: 值=%v 范围=[%v, %v]", gauges,
					c.Chart.Series[0].Data, c.Chart.GaugeMin, c.Chart.GaugeMax)
			}
		}
	}
	if gauges == 0 {
		t.Fatal("多个仪表盘.cpt 中没有仪表盘图")
	}
	t.Logf("共 %d 个仪表盘，其中 %d 个有数据", gauges, withData)
	if withData == 0 {
		t.Fatal("所有仪表盘都没有数据")
	}
	// A gauge with no data must be explained by a warning, not silently blank.
	if withData < gauges {
		found := false
		for _, w := range p.Warnings {
			if strings.Contains(w, "仪表盘") {
				found = true
			}
		}
		if !found {
			t.Errorf("有仪表盘无数据但没有相关告警: %v", p.Warnings)
		}
	}
}

// A gauge whose value column is the same as its name column still has to
// produce a value.
func TestChartGaugeSingleValue(t *testing.T) {
	charts := chartsIn(t, "demo/Phone/dashboard/驾驶舱主页_名片-phone.cpt")
	for _, c := range charts {
		if c.Kind == "gauge" {
			t.Logf("单值仪表盘: 值=%v 类别=%v", c.Series[0].Data, c.Categories)
			if len(c.Series[0].Data) == 0 {
				t.Error("单值仪表盘没有数据点")
			}
			return
		}
	}
	t.Logf("该模板中没有仪表盘图（类型: %s）", kindsOf(charts))
}

// parseMeterTarget must accept both plain numbers and formula values.
func TestParseMeterTarget(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"500", 500, true},
		{"=308.75", 308.75, true},
		{"  =80 ", 80, true},
		{"0.0", 0, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseMeterTarget(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseMeterTarget(%q) = %v,%v 期望 %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// A word cloud must read its words and weights from the WordCloud data
// definition rather than falling back to a bar chart.
// The five-number summary must follow the usual quartile convention.
func TestFiveNumberSummary(t *testing.T) {
	// 1..5 -> min 1, q1 2, median 3, q3 4, max 5
	got := fiveNumberSummary([]float64{5, 3, 1, 4, 2})
	want := []float64{1, 2, 3, 4, 5}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("五数概括[%d] = %v, 期望 %v (完整 %v)", i, got[i], want[i], got)
		}
	}
	// A single value collapses to the same number five times.
	one := fiveNumberSummary([]float64{7})
	for i, v := range one {
		if v != 7 {
			t.Errorf("单值时五数概括[%d] = %v, 期望 7", i, v)
		}
	}
	// Out-of-order input must be sorted first.
	unsorted := fiveNumberSummary([]float64{9, 1, 5, 3, 7})
	if unsorted[0] != 1 || unsorted[4] != 9 {
		t.Errorf("未排序输入的五数概括错误: %v", unsorted)
	}
	// An empty set must not panic.
	empty := fiveNumberSummary(nil)
	if len(empty) != 5 {
		t.Errorf("空输入的五数概括长度应为 5, 实际 %d", len(empty))
	}
}

// A box plot must be recognised and produce five-number summaries per box.
func TestChartBox(t *testing.T) {
	// The bundled demo template's embedded data does not decode, so the data
	// path is covered with a synthetic dataset shaped like FineReport's.
	ds := &Dataset{
		Name:        "茉莉花数据",
		ClassName:   "com.fr.data.impl.EmbeddedTableData",
		Columns:     []string{"数据项", "普通茉莉花", "宝珠茉莉花"},
		ColumnTypes: []string{"java.lang.String", "java.lang.Integer", "java.lang.Integer"},
		Rows: [][]any{
			{"花长", 10, 20},
			{"花长", 12, 22},
			{"花长", 14, 24},
			{"花长", 16, 26},
			{"花长", 18, 28},
			{"花宽", 5, 8},
			{"花宽", 7, 10},
			{"花宽", 9, 12},
		},
		Loaded: true,
	}
	xml := `<ChartDefinition><BoxTableDefinition>` +
		`<TableData class="com.fr.data.impl.NameTableData"><Name><![CDATA[茉莉花数据]]></Name></TableData>` +
		`<detailedDefinition class="com.fr.plugin.chart.box.data.VanBoxMoreNameCDDefinition">` +
		`<MoreNameCDDefinition>` +
		`<CategoryName value="数据项"/>` +
		`<ChartSummaryColumn name="普通茉莉花" customName="普通茉莉花"/>` +
		`<ChartSummaryColumn name="宝珠茉莉花" customName="宝珠茉莉花"/>` +
		`</MoreNameCDDefinition></detailedDefinition>` +
		`</BoxTableDefinition></ChartDefinition>`
	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("xml: %v", err)
	}
	inner := root.Find("ChartDefinition").Children[0]
	spec := &ChartSpec{Kind: "box", Legend: true}
	var warns []string
	parseBoxDefinition(inner, spec, map[string]*Dataset{"茉莉花数据": ds}, func(s string) {
		warns = append(warns, s)
	})
	if len(warns) > 0 {
		t.Fatalf("解析告警: %v", warns)
	}
	if len(spec.Categories) != 2 {
		t.Errorf("分类应为 2 个（花长/花宽），实际 %v", spec.Categories)
	}
	if len(spec.Series) != 2 {
		t.Fatalf("箱体系列应为 2 个，实际 %d", len(spec.Series))
	}
	for _, s := range spec.Series {
		if len(s.BoxValues) != len(spec.Categories) {
			t.Errorf("系列 %q 的箱体数 %d 与分类数 %d 不一致", s.Name, len(s.BoxValues), len(spec.Categories))
			continue
		}
		for i, v := range s.BoxValues {
			if len(v) != 5 {
				t.Errorf("系列 %q 第 %d 个箱体应有 5 个数，实际 %d", s.Name, i, len(v))
				continue
			}
			if v[0] > v[1] || v[1] > v[2] || v[2] > v[3] || v[3] > v[4] {
				t.Errorf("系列 %q 第 %d 个箱体非单调: %v", s.Name, i, v)
			}
		}
		t.Logf("系列 %q 箱体: %v", s.Name, s.BoxValues)
	}
	// The first box summarises 10,12,14,16,18.
	first := spec.Series[0].BoxValues[0]
	if math.Abs(first[0]-10) > 1e-9 || math.Abs(first[4]-18) > 1e-9 {
		t.Errorf("花长-普通茉莉花 箱体应为 [10..18]，实际 %v", first)
	}
}

// A word cloud must be recognised as such and read its word/weight columns
// from the WordCloud data definition.
//
// The bundled demo template's embedded data does not survive decoding (the
// deflate stream is incomplete — FineReport's own Inflater reports
// "not-finished" on it too, see tools/oracle/Ascii85Oracle.java), so this
// asserts the parsing behaviour rather than the data. The data path is
// covered by TestWordCloudWithData below.
func TestChartWordCloud(t *testing.T) {
	requireWebroot(t)
	tpl, err := ParseCPTFile(webroot + `/WEB-INF/reportlets/doc/Chart/WorldCloud/词云图.cpt`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The definition must name a word column and a weight column.
	var found bool
	for _, sh := range tpl.Sheets {
		for _, ct := range sh.CellList {
			if ct.ChartXML == nil {
				continue
			}
			def := ct.ChartXML.Find("ChartDefinition")
			if def == nil {
				continue
			}
			for _, n := range def.Children {
				wc := n.Find("WordCloudTableDefinition")
				if wc == nil {
					continue
				}
				found = true
				if wc.Attr("wordname") == "" {
					t.Error("词云定义缺少词列(wordname)")
				}
				if wc.Attr("wordvalue") == "" {
					t.Error("词云定义缺少权重列(wordvalue)")
				}
				t.Logf("词云定义: 词列=%q 权重列=%q", wc.Attr("wordname"), wc.Attr("wordvalue"))
			}
		}
	}
	if !found {
		t.Fatal("未找到 WordCloudTableDefinition")
	}
}

// The word cloud data path must work when the dataset does decode.
func TestWordCloudWithData(t *testing.T) {
	ds := &Dataset{
		Name:        "wc",
		ClassName:   "com.fr.data.impl.EmbeddedTableData",
		Columns:     []string{"搜索词", "浏览量(PV)"},
		ColumnTypes: []string{"java.lang.String", "java.lang.Integer"},
		Rows: [][]any{
			{"报表", 120}, {"数据", 90}, {"分析", 75}, {"可视化", 60}, {"大屏", 45},
		},
		Loaded: true,
	}
	// Build a definition node the way FineReport writes it.
	xml := `<ChartDefinition><WordCloudTableDefinition name="" wordname="搜索词" wordvalue="浏览量(PV)">` +
		`<TableData class="com.fr.data.impl.NameTableData"><Name><![CDATA[wc]]></Name></TableData>` +
		`</WordCloudTableDefinition></ChartDefinition>`
	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("xml: %v", err)
	}
	def := root.Find("ChartDefinition")
	if def == nil {
		t.Fatal("未解析到 ChartDefinition")
	}
	inner := def.Children[0]
	spec := &ChartSpec{Kind: "wordcloud", Legend: false}
	var warns []string
	parseWordCloudDefinition(inner, spec, map[string]*Dataset{"wc": ds}, func(s string) {
		warns = append(warns, s)
	})
	if len(warns) > 0 {
		t.Fatalf("解析告警: %v", warns)
	}
	if len(spec.Categories) != 5 {
		t.Errorf("词数应为 5，实际 %d", len(spec.Categories))
	}
	if len(spec.Series) != 1 || len(spec.Series[0].Data) != 5 {
		t.Fatalf("权重数据异常: %+v", spec.Series)
	}
	if spec.Categories[0] != "报表" || spec.Series[0].Data[0] != 120 {
		t.Errorf("首个词异常: %q = %v", spec.Categories[0], spec.Series[0].Data[0])
	}
	t.Logf("词云解析成功: %v = %v", spec.Categories, spec.Series[0].Data)
}

// A word cloud must not be downgraded to a bar chart.
func TestChartWordCloudNotDowngraded(t *testing.T) {
	requireWebroot(t)
	p := renderDemo(t, webroot+`/WEB-INF/reportlets/doc/Chart/WorldCloud/词云图.cpt`)
	for _, w := range p.Warnings {
		if strings.Contains(w, "降级") {
			t.Errorf("词云仍被降级: %s", w)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// No chart anywhere in the sample set may still be downgraded to a bar.
func TestNoDowngradeWarnings(t *testing.T) {
	requireWebroot(t)
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

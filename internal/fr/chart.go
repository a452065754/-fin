package fr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseChartSpec converts FineReport chart XML into a small normalised model
// that the browser renders with ECharts.
func ParseChartSpec(chart *XNode, datasets map[string]*Dataset) (*ChartSpec, []string) {
	var warns []string
	warn := func(s string) { warns = append(warns, s) }
	if chart == nil {
		return nil, []string{"图表定义缺失"}
	}
	spec := &ChartSpec{Kind: "bar", Legend: true}
	plot := chart.Child("Plot")
	if plot == nil {
		plot = chart.Find("Plot")
	}
	if plot == nil {
		return nil, []string{"图表缺少 Plot 定义"}
	}
	cls := strings.ToLower(plot.Attr("class"))
	unsupported := ""
	switch {
	case strings.Contains(cls, "column"), strings.Contains(cls, "bar2d"):
		spec.Kind = "bar"
		if a := plot.Find("VanChartColumnPlotAttr"); a != nil && a.AttrBool("isBar", false) {
			spec.Horizontal = true
		}
	case strings.Contains(cls, "line"):
		spec.Kind = "line"
	case strings.Contains(cls, "area"):
		spec.Kind = "area"
	case strings.Contains(cls, "pieplot4vanchart"), strings.Contains(cls, "chartattr.pieplot"):
		spec.Kind = "pie"
	case strings.Contains(cls, "funnel"):
		spec.Kind = "funnel"
	case strings.Contains(cls, "scatter"):
		spec.Kind = "scatter"
	case strings.Contains(cls, "bubble"):
		// A bubble chart needs both a value axis and a radius dimension.
		// FineReport exposes the radius as combinedSize; when the template
		// only supplies one value column we fall back to a bubble sized by
		// the value itself, which is the common dashboard usage.
		spec.Kind = "bubble"
	case strings.Contains(cls, "radar"):
		spec.Kind = "radar"
	case strings.Contains(cls, "gauge"):
		spec.Kind = "gauge"
	case strings.Contains(cls, "heatmap"):
		// Must precede the map branch: "heatmap" contains "map".
		spec.Kind = "heatmap"
	case strings.Contains(cls, "drillmap"):
		spec.Kind = "map"
		spec.DrillDown = true
	case strings.Contains(cls, "map"):
		spec.Kind = "map"
		spec.MapArea = mapArea(plot)
	case strings.Contains(cls, "custom"):
		// Combo: FineReport mixes several plot kinds in one chart. We keep the
		// data intact and let the browser draw bar/line/area per series.
		spec.Kind = "combo"
		spec.Combo = true
	case strings.Contains(cls, "multipie"), strings.Contains(cls, "multi"):
		// Multi-layer pie is a nested donut: rings per series.
		spec.Kind = "multipie"
	case strings.Contains(cls, "wordcloud"):
		spec.Kind = "wordcloud"
	case strings.Contains(cls, "box"):
		spec.Kind = "box"
	case strings.Contains(cls, "treemap"):
		spec.Kind = "treemap"
	case strings.Contains(cls, "gantt"):
		// Gantt is a bar chart on a time axis, which is the closest faithful
		// rendering without a dedicated ECharts gantt component.
		spec.Kind = "gantt"
		spec.Horizontal = true
	case strings.Contains(cls, "structure"):
		// Hierarchy chart: closest faithful ECharts form is a sunburst.
		spec.Kind = "sunburst"
	default:
		spec.Kind = "bar"
		if cls != "" {
			unsupported = "未识别的图表类型（" + cls + "），已按柱形图绘制"
		}
	}
	if unsupported != "" {
		warn(unsupported)
		spec.Warning = unsupported
	}
	// Stacking is recorded in either plot type or axis condition attributes.
	if rect := plot.Find("VanChartRectanglePlotAttr"); rect != nil {
		if strings.EqualFold(rect.Attr("vanChartPlotType"), "stack") {
			spec.Stack = true
		}
	}
	for _, a := range plot.FindAll("Attr") {
		if a.AttrBool("stacked", false) || a.AttrBool("percentStacked", false) {
			spec.Stack = true
		}
	}
	switch spec.Kind {
	case "pie", "funnel", "multipie", "sunburst", "wordcloud", "treemap", "gauge", "map", "heatmap":
		// Ring, part-to-whole and geographic charts have no stacking concept.
		spec.Stack = false
	}
	spec.Colors = parseChartColors(plot)
	parseChartTitle(chart, spec)
	parseChartLegend(chart, spec)
	parseGaugeRange(plot, spec)
	parseChartData(chart, spec, datasets, warn)
	decorateSeries(spec, plot)
	if len(spec.Series) == 0 {
		warn("图表没有可用的数据系列")
	}
	// Radar needs at least three axes to form a polygon.
	if spec.Kind == "radar" && len(spec.Categories) < 3 {
		spec.Kind = "bar"
		warn("雷达图数据不足，已降级为柱形图")
	}
	return spec, warns
}

// parseGaugeRange reads the gauge scale from the plot. FineReport stores it on
// an MC_Attr node (main colour / range) next to a list of AC_Attr segments that
// colour the value bands.
//
//	<MC_Attr minValue="0.0" maxValue="100.0" useType="0" areaNumber="5">
//	<AC_Attr minValue="=80" maxValue="=100">
//
// useType=1 means the range is fixed as written; useType=0 lets the renderer
// derive it from the data.
func parseGaugeRange(plot *XNode, spec *ChartSpec) {
	if spec.Kind != "gauge" {
		return
	}
	mc := plot.Find("MC_Attr")
	if mc == nil {
		return
	}
	minV, okMin := parseMeterTarget(mc.Attr("minValue"))
	maxV, okMax := parseMeterTarget(mc.Attr("maxValue"))
	if okMin && okMax && maxV > minV {
		spec.GaugeMin = minV
		spec.GaugeMax = maxV
		spec.GaugeAuto = mc.Attr("useType") == "0"
	}
	// Segment thresholds drive the coloured bands of the dial. FineReport
	// writes them in two shapes: the colour either sits in a `color` attribute
	// or in a <color><FineColor/></color> child.
	for _, ac := range plot.FindAll("AC_Attr") {
		lo, okLo := parseMeterTarget(ac.Attr("minValue"))
		hi, okHi := parseMeterTarget(ac.Attr("maxValue"))
		if !okLo || !okHi {
			continue
		}
		// Some templates write the band upside down (min above max).
		if hi < lo {
			lo, hi = hi, lo
		}
		color := ""
		if v := ac.Attr("color"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				color = FRColor(n)
			}
		}
		if color == "" {
			if c := ac.Child("color"); c != nil {
				if fc := c.Child("FineColor"); fc != nil {
					if v, err := strconv.ParseInt(fc.Attr("color"), 10, 64); err == nil {
						color = FRColor(v)
					}
				}
			}
		}
		if color == "" {
			if fc := ac.Child("FineColor"); fc != nil {
				if v, err := strconv.ParseInt(fc.Attr("color"), 10, 64); err == nil {
					color = FRColor(v)
				}
			}
		}
		spec.GaugeBands = append(spec.GaugeBands, GaugeBand{
			Min: lo, Max: hi, Color: color,
		})
	}
}

// mapArea reads the geography selector of a map plot. FineReport stores it on
// the plot as a plain string; anything unrecognised means "whole country".
func mapArea(plot *XNode) string {
	for _, a := range plot.FindAll("Attr") {
		for _, key := range []string{"mapType", "areaName", "mapArea", "typeName"} {
			if v := strings.TrimSpace(a.Attr(key)); v != "" {
				return v
			}
		}
	}
	return ""
}

func parseChartColors(plot *XNode) []string {
	root := plot
	if fs := plot.Find("newPlotFillStyle"); fs != nil {
		root = fs
	}
	var out []string
	seen := map[string]bool{}
	for _, oc := range root.FindAll("OColor") {
		v, err := strconv.ParseInt(oc.Attr("colvalue"), 10, 64)
		if err != nil {
			continue
		}
		c := FRColor(v)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func parseChartTitle(chart *XNode, spec *ChartSpec) {
	tn := chart.Child("Title4VanChart")
	if tn == nil {
		tn = chart.Find("Title4VanChart")
	}
	if tn == nil {
		return
	}
	title := tn.Child("Title")
	if title == nil {
		title = tn.Find("Title")
	}
	if title == nil {
		return
	}
	if tv := title.Child("TitleVisible"); tv != nil && !tv.AttrBool("value", true) {
		return
	}
	if o := title.Child("O"); o != nil {
		spec.Title = strings.TrimSpace(o.TextTrim())
	} else {
		spec.Title = strings.TrimSpace(title.TextTrim())
	}
	if f := title.Find("FRFont"); f != nil {
		if size, err := strconv.ParseFloat(f.Attr("size"), 64); err == nil {
			spec.TitleSize = FontSizePx(size)
		}
		if fg := f.Attr("foreground"); fg != "" {
			if v, err := strconv.ParseInt(fg, 10, 64); err == nil {
				spec.TitleColor = FRColor(v)
			}
		}
	}
}

func parseChartLegend(chart *XNode, spec *ChartSpec) {
	ln := chart.Child("Legend4VanChart")
	if ln == nil {
		ln = chart.Find("Legend4VanChart")
	}
	if ln == nil {
		return
	}
	legend := ln.Child("Legend")
	if legend == nil {
		legend = ln.Find("Legend")
	}
	if legend == nil {
		return
	}
	if attr := legend.Find("Attr"); attr != nil {
		if !attr.AttrBool("visible", true) {
			spec.Legend = false
		}
		if pos := attr.AttrInt("position", -1); pos >= 0 {
			switch pos {
			case 0:
				spec.LegendPos = "center"
			case 1:
				spec.LegendPos = "top"
			case 2:
				spec.LegendPos = "left"
			case 3:
				spec.LegendPos = "bottom"
			case 4:
				spec.LegendPos = "right"
			}
		}
	}
}

type chartDef struct {
	dataset    *Dataset
	category   string
	seriesCol  string
	valueCol   string
	funcName   string
	valueCols  []string
	valueNames []string
	extraWarn  string
}

// toFloatOrDate converts a dataset cell into a number, additionally
// accepting the timestamp strings FineReport embeds for date columns. Dates
// become days since an arbitrary epoch, which is enough to place a gantt bar
// on a relative timeline.
func toFloatOrDate(v any) (float64, bool) {
	if f, ok := toFloat(v); ok {
		return f, true
	}
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	if ms, ok := parseDate(s); ok {
		return float64(ms) / 86400000, true
	}
	return 0, false
}

// dateLayouts are tried in order; the first that parses wins.
var dateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
	"2006年01月02日",
}

func parseDate(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}

// unwrapFirst returns the first direct child with the given name, or nil.
func unwrapFirst(n *XNode, name string) *XNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return n.Find(name)
}

// pickCustomDefinition chooses which DefinitionMap of a combo chart to read.
// FineReport stores one entry per plot kind; the first one that resolves to an
// existing dataset is the one carrying the actual values.
func pickCustomDefinition(n *XNode, datasets map[string]*Dataset) *XNode {
	// <CustomDefinition><DefinitionMapList><DefinitionMap key="column">
	//     <MoreNameCDDefinition>...
	var fallback *XNode
	var maps []*XNode
	if ml := n.Find("DefinitionMapList"); ml != nil {
		maps = ml.ChildAll("DefinitionMap")
	}
	if len(maps) == 0 {
		maps = n.ChildAll("DefinitionMap")
	}
	for _, dm := range maps {
		for _, def := range dm.Children {
			if def == nil || !strings.Contains(def.Name, "Definition") {
				continue
			}
			ds := datasetOf(def, datasets)
			if ds != nil && ds.Loaded && len(ds.Rows) > 0 {
				return def
			}
			if fallback == nil {
				fallback = def
			}
		}
	}
	return fallback
}

// parseBubbleDefinition handles BubbleTableDefinition, which names its three
// value axes directly instead of going through the generic category/value
// shape. Returns true when the chart was fully populated.
func parseBubbleDefinition(inner *XNode, spec *ChartSpec, datasets map[string]*Dataset, warn func(string)) bool {
	b := inner.Find("BubbleTableDefinition201109")
	if b == nil {
		return false
	}
	ds := datasetOf(inner, datasets)
	if ds == nil {
		warn("气泡图引用的数据集不存在")
		return false
	}
	if !ds.Loaded || len(ds.Rows) == 0 {
		warn("气泡图数据集没有数据")
		return false
	}
	xCol := b.Attr("bubbleX")
	yCol := b.Attr("bubbleY")
	sizeCol := b.Attr("bubbleSize")
	nameCol := b.Attr("seriesName")

	xIdx := ds.ColumnIndex(xCol)
	yIdx := ds.ColumnIndex(yCol)
	sIdx := ds.ColumnIndex(sizeCol)
	if xIdx < 0 || yIdx < 0 {
		warn(fmt.Sprintf("气泡图数据集 %s 缺少 X/Y 列（%s / %s）", ds.Name, xCol, yCol))
		return false
	}
	// The bubble label column is optional; fall back to the series name.
	labelIdx := -1
	if nameCol != "" {
		labelIdx = ds.ColumnIndex(nameCol)
	}

	names := make([]string, 0, len(ds.Rows))
	xVals := make([]float64, 0, len(ds.Rows))
	yVals := make([]float64, 0, len(ds.Rows))
	sVals := make([]float64, 0, len(ds.Rows))
	for _, row := range ds.Rows {
		x, okx := toFloat(cellValue(row, xIdx))
		y, oky := toFloat(cellValue(row, yIdx))
		if !okx || !oky {
			continue
		}
		label := ""
		if labelIdx >= 0 {
			label = stringOf(cellValue(row, labelIdx))
		}
		if label == "" {
			label = fmt.Sprintf("%g/%g", x, y)
		}
		size := 0.0
		if sIdx >= 0 {
			if f, ok := toFloat(cellValue(row, sIdx)); ok {
				size = f
			}
		}
		names = append(names, label)
		xVals = append(xVals, x)
		yVals = append(yVals, y)
		sVals = append(sVals, size)
	}
	if len(xVals) == 0 {
		warn("气泡图没有有效的数值点")
		return false
	}

	// Normalise the radius so the largest bubble stays readable regardless of
	// the units the template uses.
	var maxS float64
	for _, v := range sVals {
		if v > maxS {
			maxS = v
		}
	}
	sizes := make([]float64, len(sVals))
	for i, v := range sVals {
		if maxS > 0 && v > 0 {
			sizes[i] = 8 + 34*v/maxS
		}
	}

	spec.Series = append(spec.Series, ChartSeries{
		Name: firstNonEmpty(nameCol, ds.Name),
		Data: yVals,
		Size: sizes,
		Type: "scatter",
	})
	spec.Categories = names
	spec.AxisLabel = []string{xCol, yCol}
	spec.AxisValues = xVals
	return true
}

// parseGanttDefinition handles VanGanttTableDefinition: each row becomes one
// bar spanning start..end, grouped by the process (series) column.
func parseGanttDefinition(inner *XNode, spec *ChartSpec, datasets map[string]*Dataset, warn func(string)) bool {
	g := inner.Find("VanGanttTableDefinition")
	if g == nil {
		return false
	}
	attr := g.Child("GanttAttr")
	if attr == nil {
		attr = g.Find("GanttAttr")
	}
	if attr == nil {
		return false
	}
	ds := datasetOf(g, datasets)
	if ds == nil || !ds.Loaded || len(ds.Rows) == 0 {
		return false
	}
	processCol := attr.Attr("firstprocess")
	seriesCol := attr.Attr("seriesname")
	startCol := attr.Attr("starttime")
	endCol := attr.Attr("endtime")
	progressCol := attr.Attr("progress")
	labelCol := attr.Attr("linkid")

	sIdx := ds.ColumnIndex(seriesCol)
	pIdx := ds.ColumnIndex(processCol)
	stIdx := ds.ColumnIndex(startCol)
	etIdx := ds.ColumnIndex(endCol)
	prIdx := -1
	if progressCol != "" {
		prIdx = ds.ColumnIndex(progressCol)
	}
	lIdx := -1
	if labelCol != "" {
		lIdx = ds.ColumnIndex(labelCol)
	}
	if stIdx < 0 || etIdx < 0 {
		warn(fmt.Sprintf("甘特图数据集 %s 缺少开始/结束列（%s / %s）", ds.Name, startCol, endCol))
		return false
	}

	// Group rows by the process so each process becomes one series; the
	// seriesname column splits further into sub-series (e.g. actual vs plan).
	type bar struct {
		start, end float64
		label      string
		progress   float64
		sub        string
	}
	subOf := map[string][]string{}
	subSeen := map[string]map[string]bool{}
	order := []string{}
	groups := map[string][]bar{}
	for _, row := range ds.Rows {
		st, ok1 := toFloatOrDate(cellValue(row, stIdx))
		et, ok2 := toFloatOrDate(cellValue(row, etIdx))
		if !ok1 || !ok2 {
			continue
		}
		key := ""
		if pIdx >= 0 {
			key = stringOf(cellValue(row, pIdx))
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
			subSeen[key] = map[string]bool{}
		}
		sub := ""
		if sIdx >= 0 {
			sub = stringOf(cellValue(row, sIdx))
		}
		if !subSeen[key][sub] {
			subSeen[key][sub] = true
			subOf[key] = append(subOf[key], sub)
		}
		label := ""
		if lIdx >= 0 {
			label = stringOf(cellValue(row, lIdx))
		}
		prog := 0.0
		if prIdx >= 0 {
			if f, ok := toFloat(cellValue(row, prIdx)); ok {
				prog = f
			}
		}
		groups[key] = append(groups[key], bar{st, et, label, prog, sub})
	}
	if len(order) == 0 {
		warn("甘特图没有有效的任务数据")
		return false
	}
	// A gantt bar is drawn as [start, duration] in ECharts terms. The earliest
	// start value becomes the custom axis origin so the timeline is absolute.
	var minStart float64
	for _, key := range order {
		for _, b := range groups[key] {
			if b.start < minStart {
				minStart = b.start
			}
		}
	}
	for _, key := range order {
		bars := groups[key]
		name := key
		if name == "" {
			name = processCol
		}
		// When the template defines sub-series (seriesname), emit one ECharts
		// series per sub-group so they stack within the process.
		subs := subOf[key]
		if len(subs) <= 1 {
			cs := ChartSeries{Name: name, Type: "bar"}
			for _, b := range bars {
				cs.Data = append(cs.Data, b.end-b.start)
			}
			spec.Series = append(spec.Series, cs)
			continue
		}
		for _, sub := range subs {
			label := name
			if sub != "" {
				label = name + " · " + sub
			}
			cs := ChartSeries{Name: label, Type: "bar"}
			for _, b := range bars {
				if b.sub == sub {
					cs.Data = append(cs.Data, b.end-b.start)
				}
			}
			if len(cs.Data) > 0 {
				spec.Series = append(spec.Series, cs)
			}
		}
	}
	spec.AxisValues = []float64{minStart}
	spec.Horizontal = true
	return true
}

// parseStructureDefinition handles StructureTableDefinition: a parent/child
// tree rendered as a sunburst.
func parseStructureDefinition(inner *XNode, spec *ChartSpec, datasets map[string]*Dataset, warn func(string)) bool {
	sa := inner.Child("StructureAttr")
	if sa == nil {
		sa = inner.Find("StructureAttr")
	}
	if sa == nil {
		return false
	}
	ds := datasetOf(inner, datasets)
	if ds == nil || !ds.Loaded || len(ds.Rows) == 0 {
		return false
	}
	idCol := sa.Attr("nodeid")
	parentCol := sa.Attr("parentid")
	nameCol := sa.Attr("nodename")
	valueCol := sa.Attr("nodevalue")
	if nameCol == "" {
		return false
	}
	nIdx := ds.ColumnIndex(nameCol)
	pIdx := ds.ColumnIndex(parentCol)
	iIdx := ds.ColumnIndex(idCol)
	vIdx := ds.ColumnIndex(valueCol)
	if nIdx < 0 {
		warn(fmt.Sprintf("层级图数据集 %s 缺少名称列 %s", ds.Name, nameCol))
		return false
	}
	// Children are emitted as categories; the parent/child link is preserved
	// through the labels so the browser can rebuild the tree.
	var cats []string
	var vals []float64
	for _, row := range ds.Rows {
		name := stringOf(cellValue(row, nIdx))
		if strings.TrimSpace(name) == "" {
			continue
		}
		if pIdx >= 0 && iIdx >= 0 {
			pid := stringOf(cellValue(row, pIdx))
			if pid != "" && pid != "0" && pid != "-1" {
				name = pid + " / " + name
			}
		}
		v := 1.0
		if vIdx >= 0 {
			if f, ok := toFloat(cellValue(row, vIdx)); ok {
				v = f
			}
		}
		cats = append(cats, name)
		vals = append(vals, v)
	}
	if len(cats) == 0 {
		warn("层级图没有有效的节点")
		return false
	}
	spec.Categories = cats
	spec.Series = append(spec.Series, ChartSeries{
		Name: firstNonEmpty(sa.Attr("seriesname"), nameCol),
		Data: vals,
		Type: "sunburst",
	})
	return true
}

// parseMeterDefinition handles MeterTableDefinition, the data source of a
// gauge chart. It names a category column and a value column directly, and
// optionally carries a custom target value.
//
// FineReport's shape:
//
//	<MeterTableDefinition>
//	  <TableData><Name>ds1</Name></TableData>
//	  <MeterTable201109 meterType="1" name="类别名称" value="库存量"
//	                     custom="true" customTarget="500"/>
//	</MeterTableDefinition>
func parseMeterDefinition(inner *XNode, spec *ChartSpec, datasets map[string]*Dataset, warn func(string)) bool {
	mt := inner.Child("MeterTable201109")
	if mt == nil {
		mt = inner.Find("MeterTable201109")
	}
	if mt == nil {
		return false
	}
	ds := datasetOf(inner, datasets)
	if ds == nil {
		warn("仪表盘引用的数据集不存在")
		return false
	}
	if !ds.Loaded || len(ds.Rows) == 0 {
		warn("仪表盘数据集没有数据")
		return false
	}
	nameCol := mt.Attr("name")
	valueCol := mt.Attr("value")
	if valueCol == "" {
		warn("仪表盘定义缺少数值列")
		return false
	}
	nIdx := ds.ColumnIndex(nameCol)
	vIdx := ds.ColumnIndex(valueCol)
	if vIdx < 0 {
		warn(fmt.Sprintf("仪表盘数据集 %s 缺少数值列 %s", ds.Name, valueCol))
		return false
	}

	// Each row is one gauge pointer. A gauge usually shows a single value, but
	// templates may carry several, so keep them all.
	var cats []string
	var vals []float64
	for _, row := range ds.Rows {
		v, ok := toFloat(cellValue(row, vIdx))
		if !ok {
			continue
		}
		label := ""
		if nIdx >= 0 {
			label = stringOf(cellValue(row, nIdx))
		}
		if label == "" {
			label = valueCol
		}
		cats = append(cats, label)
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		warn("仪表盘没有有效的数值")
		return false
	}

	spec.Categories = cats
	spec.Series = append(spec.Series, ChartSeries{
		Name: firstNonEmpty(nameCol, valueCol),
		Data: vals,
		Type: "gauge",
	})

	// A custom target becomes the gauge's reference mark.
	if mt.AttrBool("custom", false) {
		if t := mt.Attr("customTarget"); t != "" {
			if f, ok := parseMeterTarget(t); ok {
				spec.AxisValues = []float64{f}
			}
		}
	}
	return true
}

// parseMeterTarget reads a target value that may be written as a plain number
// or as a FineReport formula such as "=308.75".
func parseMeterTarget(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	return toFloat(s)
}

func datasetOf(n *XNode, datasets map[string]*Dataset) *Dataset {
	if n == nil {
		return nil
	}
	if td := n.Child("TableData"); td != nil {
		if name := td.Child("Name"); name != nil {
			return datasets[name.TextTrim()]
		}
	}
	if name := n.Child("Name"); name != nil {
		return datasets[name.TextTrim()]
	}
	// Wrappers such as <dataDefinition class="...VanGanttTableDefinition">
	// put the real definition one level down.
	for _, c := range n.Children {
		if c == nil {
			continue
		}
		if ds := datasetOf(c, datasets); ds != nil {
			return ds
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseChartData(chart *XNode, spec *ChartSpec, datasets map[string]*Dataset, warn func(string)) {
	def := chart.Child("ChartDefinition")
	if def == nil {
		def = chart.Find("ChartDefinition")
	}
	if def == nil {
		warn("图表缺少数据定义")
		return
	}
	// Pick the definition node. Wrapper elements such as
	// <dataDefinition class="...VanGanttTableDefinition"> carry a class
	// attribute but no dataset of their own, so a node holding a TableData is
	// always the better candidate.
	var inner *XNode
	var wrapper *XNode
	for _, n := range def.Children {
		lname := strings.ToLower(n.Name)
		if !strings.Contains(n.Name, "Definition") {
			continue
		}
		if lname == "datadefinition" || lname == "areadefinition" || lname == "linkdefinition" {
			if wrapper == nil {
				wrapper = n
			}
			continue
		}
		// A combo chart puts its definitions one level down, inside
		// <CustomDefinition><DefinitionMapList><DefinitionMap>.
		if ml := n.Find("DefinitionMapList"); ml != nil {
			inner = n
			break
		}
		// Prefer a node that owns the dataset reference or its own attr schema.
		if n.Child("TableData") != nil || n.Child("GanttAttr") != nil ||
			n.Child("StructureAttr") != nil || n.Child("BubbleTableDefinition201109") != nil {
			inner = n
			break
		}
		if inner == nil {
			inner = n
		}
	}
	if inner == nil {
		inner = wrapper
	}
	if inner == nil {
		warn("图表数据定义无法识别")
		return
	}
	name := inner.Name

	// Several chart kinds wrap a nested definition instead of using
	// One/MoreName directly. Unwrap them so the shared parsing below applies.
	switch {
	case strings.Contains(name, "Bubble"):
		// BubbleTableDefinition delegates to a BubbleTableDefinition201109
		// node that names the X, Y and radius columns explicitly.
		if parseBubbleDefinition(inner, spec, datasets, warn) {
			return
		}
		if n := unwrapFirst(inner, "BubbleTableDefinition201109"); n != nil {
			inner = n
		}
	case strings.Contains(name, "Gantt"):
		// VanGanttTableDefinition names a start/end pair plus an optional
		// progress column and a link id.
		if parseGanttDefinition(inner, spec, datasets, warn) {
			return
		}
		if n := unwrapFirst(inner, "VanGanttTableDefinition"); n != nil {
			inner = n
		}
	case strings.Contains(name, "Structure"):
		// StructureTableDefinition is a parent/child hierarchy rather than a
		// category/value grid, so it feeds a sunburst.
		if parseStructureDefinition(inner, spec, datasets, warn) {
			return
		}
		if n := unwrapFirst(inner, "MoreNameCDDefinition"); n != nil {
			inner = n
		}
	case strings.Contains(name, "Meter"):
		// MeterTableDefinition names a category column and a value column
		// directly; the gauge range lives on the plot instead.
		if parseMeterDefinition(inner, spec, datasets, warn) {
			return
		}
	case strings.Contains(name, "ReportDataDefinition"), strings.Contains(name, "ReportDefinition"):
		// These definitions read the report's own cells through formulas such
		// as =C3. The values are not available while parsing, so the chart is
		// marked and resolved by the renderer in a second pass.
		if src := parseChartCellSource(inner); src != nil {
			spec.cellSource = src
			spec.NeedsCellSource = true
			spec.Series = nil
			return
		}
	case strings.Contains(name, "VanMap"), strings.Contains(name, "DillMap"), strings.Contains(name, "MapDefinition"):
		// VanMapDefinition wraps the real definition in an areaDefinition node.
		if ad := inner.Find("areaDefinition"); ad != nil {
			inner = ad
		}
		if n := unwrapFirst(inner, "MoreNameCDDefinition"); n != nil {
			inner = n
		} else if n := unwrapFirst(inner, "OneValueCDDefinition"); n != nil {
			inner = n
		}
	case strings.Contains(name, "Custom"):
		// CustomDefinition holds one DefinitionMap per plot kind; the column
		// definition is the one that matches the dataset we can resolve.
		if n := pickCustomDefinition(inner, datasets); n != nil {
			inner = n
			name = inner.Name
		}
	case strings.Contains(name, "MultiPie"):
		if n := unwrapFirst(inner, "MoreNameCDDefinition"); n != nil {
			inner = n
		}
	}
	if inner == nil {
		warn("图表数据定义无法解析")
		return
	}
	name = inner.Name

	cd := chartDef{funcName: inner.Attr("function")}
	if td := inner.Child("TableData"); td != nil {
		if n := td.Child("Name"); n != nil {
			cd.dataset = datasets[n.TextTrim()]
		}
	}
	if cd.dataset == nil {
		if n := inner.Child("Name"); n != nil {
			cd.dataset = datasets[n.TextTrim()]
		}
	}
	if cat := inner.Child("CategoryName"); cat != nil {
		cd.category = cat.Attr("value")
	}
	switch {
	case strings.Contains(name, "MoreName"):
		for _, sc := range inner.ChildAll("ChartSummaryColumn") {
			cd.valueCols = append(cd.valueCols, sc.Attr("name"))
			label := sc.Attr("customName")
			if label == "" {
				label = sc.Attr("name")
			}
			cd.valueNames = append(cd.valueNames, label)
		}
	case strings.Contains(name, "OneValue"):
		cd.seriesCol = inner.Attr("seriesName")
		cd.valueCol = inner.Attr("valueName")
		if fn := inner.Attr("function"); fn != "" {
			cd.funcName = fn
		}
	default:
		// Nested definitions appear inside the specialised wrappers.
		if nested := unwrapFirst(inner, "MoreNameCDDefinition"); nested != nil && nested != inner {
			inner = nested
			name = inner.Name
		} else if nested := unwrapFirst(inner, "OneValueCDDefinition"); nested != nil && nested != inner {
			inner = nested
			name = inner.Name
		} else if inner.Name != "MoreNameCDDefinition" && inner.Name != "OneValueCDDefinition" {
			warn("暂不支持此图表数据定义（" + inner.Name + "）")
			return
		}
	}
	if cd.dataset == nil {
		warn("图表引用的数据集不存在")
		return
	}
	if !cd.dataset.Loaded || len(cd.dataset.Rows) == 0 {
		warn("图表数据集没有数据")
		return
	}
	catIdx := cd.dataset.ColumnIndex(cd.category)
	if catIdx < 0 && cd.category != "" && !strings.Contains(name, "OneValue") {
		warn(fmt.Sprintf("图表数据集 %s 缺少分类列 %s", cd.dataset.Name, cd.category))
	}
	if strings.Contains(name, "OneValue") {
		parseOneValue(spec, cd, catIdx, warn)
	} else {
		parseMoreName(spec, cd, catIdx, warn)
	}
}

func parseOneValue(spec *ChartSpec, cd chartDef, catIdx int, warn func(string)) {
	sIdx := cd.dataset.ColumnIndex(cd.seriesCol)
	vIdx := cd.dataset.ColumnIndex(cd.valueCol)
	if vIdx < 0 {
		warn(fmt.Sprintf("图表数据集 %s 缺少数值列 %s", cd.dataset.Name, cd.valueCol))
		return
	}
	if catIdx < 0 {
		// FineReport uses "无" (or omits CategoryName) for single-dimension
		// charts: the series column supplies the slice categories.
		var cats []string
		seen := map[string]int{}
		sums := []float64{}
		for _, row := range cd.dataset.Rows {
			var cat string
			if sIdx >= 0 {
				cat = stringOf(cellValue(row, sIdx))
			} else {
				cat = strconv.Itoa(len(cats) + 1)
			}
			idx, ok := seen[cat]
			if !ok {
				idx = len(cats)
				seen[cat] = idx
				cats = append(cats, cat)
				sums = append(sums, 0)
			}
			if f, ok := toFloat(cellValue(row, vIdx)); ok {
				sums[idx] += f
			}
		}
		label := cd.valueCol
		if label == "" {
			label = cd.seriesCol
		}
		spec.Categories = cats
		spec.Series = append(spec.Series, ChartSeries{Name: label, Data: sums})
		return
	}
	var cats []string
	catSeen := map[string]int{}
	seriesOrder := []string{}
	seriesSeen := map[string]bool{}
	values := map[string]map[string][]float64{}
	for _, row := range cd.dataset.Rows {
		cat := stringOf(cellValue(row, catIdx))
		if _, ok := catSeen[cat]; !ok {
			catSeen[cat] = len(cats)
			cats = append(cats, cat)
		}
		ser := ""
		if sIdx >= 0 {
			ser = stringOf(cellValue(row, sIdx))
		}
		if !seriesSeen[ser] {
			seriesSeen[ser] = true
			seriesOrder = append(seriesOrder, ser)
		}
		if values[ser] == nil {
			values[ser] = map[string][]float64{}
		}
		if f, ok := toFloat(cellValue(row, vIdx)); ok {
			values[ser][cat] = append(values[ser][cat], f)
		}
	}
	// A definition without an explicit series column is a single series chart.
	if len(seriesOrder) == 0 {
		seriesOrder = append(seriesOrder, "")
	}
	for _, ser := range seriesOrder {
		label := ser
		if label == "" {
			label = cd.valueCol
		}
		cs := ChartSeries{Name: label}
		for _, cat := range cats {
			cs.Data = append(cs.Data, aggregate(values[ser][cat], cd.funcName))
		}
		spec.Series = append(spec.Series, cs)
	}
	spec.Categories = cats
}

func parseMoreName(spec *ChartSpec, cd chartDef, catIdx int, warn func(string)) {
	var cats []string
	catSeen := map[string]bool{}
	idx := map[string]int{}
	var valueIdx []int
	for _, col := range cd.valueCols {
		i := cd.dataset.ColumnIndex(col)
		if i < 0 {
			warn(fmt.Sprintf("图表数据集 %s 缺少列 %s", cd.dataset.Name, col))
		}
		valueIdx = append(valueIdx, i)
	}
	acc := make([][]map[string][]float64, len(cd.valueCols))
	for i := range acc {
		acc[i] = make([]map[string][]float64, 0)
	}
	for _, row := range cd.dataset.Rows {
		cat := ""
		if catIdx >= 0 {
			cat = stringOf(cellValue(row, catIdx))
		} else {
			cat = strconv.Itoa(len(cats) + 1)
		}
		if !catSeen[cat] {
			catSeen[cat] = true
			idx[cat] = len(cats)
			cats = append(cats, cat)
			for i := range acc {
				acc[i] = append(acc[i], map[string][]float64{})
			}
		}
		ci := idx[cat]
		for i, vi := range valueIdx {
			if vi < 0 {
				continue
			}
			if f, ok := toFloat(cellValue(row, vi)); ok {
				acc[i][ci][cat] = append(acc[i][ci][cat], f)
			}
		}
	}
	for i := range cd.valueCols {
		name := cd.valueNames[i]
		cs := ChartSeries{Name: name}
		for ci := range cats {
			cs.Data = append(cs.Data, aggregate(acc[i][ci][cats[ci]], cd.funcName))
		}
		spec.Series = append(spec.Series, cs)
	}
	spec.Categories = cats
}

// decorateSeries fills in the per-series fields that ECharts needs for the
// chart kinds that are not a plain value-per-category plot.
func decorateSeries(spec *ChartSpec, plot *XNode) {
	switch spec.Kind {
	case "combo":
		// FineReport lets each column of a combo chart use a different plot
		// kind, recorded in CustomPlotList. When that list is missing we fall
		// back to the conventional split: first column as bars, rest as lines.
		kinds := comboKindsFromPlot(plot)
		for i := range spec.Series {
			k := "bar"
			switch {
			case i < len(kinds) && kinds[i] != "":
				k = kinds[i]
			case len(spec.Series) == 2 && i == 1:
				k = "line"
			case i > 0:
				k = "line"
			}
			if k == "column" {
				k = "bar"
			}
			spec.Series[i].Type = k
			// With three or more series the conventional combo layout puts the
			// last one on the right-hand axis so it is not flattened against
			// bars of a very different magnitude.
			if len(spec.Series) >= 3 && i == len(spec.Series)-1 {
				spec.Series[i].Axis = "y1"
			}
		}
		if spec.Stack {
			// Stacking and mixing plot kinds contradict each other.
			spec.Stack = false
		}
	case "bubble":
		// parseBubbleDefinition already sized the points from the template's
		// own bubbleSize column. Only fall back to sizing by value when that
		// dedicated path did not run (generic category/value definition).
		for i := range spec.Series {
			if len(spec.Series[i].Size) == len(spec.Series[i].Data) {
				continue
			}
			var maxV float64
			for _, v := range spec.Series[i].Data {
				if v > maxV {
					maxV = v
				}
			}
			sizes := make([]float64, 0, len(spec.Series[i].Data))
			for _, v := range spec.Series[i].Data {
				size := 0.0
				if maxV > 0 && v > 0 {
					size = 8 + 34*v/maxV
				}
				sizes = append(sizes, size)
			}
			spec.Series[i].Size = sizes
			if spec.Series[i].Type == "" {
				spec.Series[i].Type = "scatter"
			}
		}
	case "multipie", "wordcloud", "treemap", "heatmap", "sunburst":
		// These read a single numeric column per series; a bar rendering would
		// be wrong, so mark them for the browser's dedicated renderer.
		for i := range spec.Series {
			if spec.Series[i].Type == "" {
				spec.Series[i].Type = spec.Kind
			}
		}
	}
}

// comboKindsFromPlot reads the per-series plot kinds of a combo chart.
// FineReport stores them as a CustomPlotList holding one VanChartPlot child per
// value column, in series order; each child's class names the plot kind.
func comboKindsFromPlot(plot *XNode) []string {
	list := plot.Find("CustomPlotList")
	if list == nil {
		return nil
	}
	var out []string
	for _, p := range list.ChildAll("VanChartPlot") {
		out = append(out, plotKindOf(p.Attr("class")))
	}
	return out
}

// plotKindOf maps a FineReport plot class onto an ECharts series type. It
// returns "" for classes with no ECharts equivalent so the caller can fall
// back to its own default.
func plotKindOf(class string) string {
	c := strings.ToLower(class)
	switch {
	case strings.Contains(c, "column"), strings.Contains(c, "bar2d"):
		return "bar"
	case strings.Contains(c, "line"):
		return "line"
	case strings.Contains(c, "area"):
		return "area"
	case strings.Contains(c, "scatter"):
		return "scatter"
	case strings.Contains(c, "bubble"):
		return "bubble"
	case strings.Contains(c, "radar"):
		return "radar"
	case strings.Contains(c, "pie"):
		return "pie"
	default:
		return ""
	}
}

func aggregate(vals []float64, fn string) float64 {
	if len(vals) == 0 {
		return 0
	}
	lf := strings.ToLower(fn)
	switch {
	case strings.Contains(lf, "average"):
		s := 0.0
		for _, v := range vals {
			s += v
		}
		return s / float64(len(vals))
	case strings.Contains(lf, "count"):
		return float64(len(vals))
	case strings.Contains(lf, "max"):
		m := vals[0]
		for _, v := range vals[1:] {
			if v > m {
				m = v
			}
		}
		return m
	case strings.Contains(lf, "min"):
		m := vals[0]
		for _, v := range vals[1:] {
			if v < m {
				m = v
			}
		}
		return m
	default:
		s := 0.0
		for _, v := range vals {
			s += v
		}
		return s
	}
}

func cellValue(row []any, i int) any {
	if i < 0 || i >= len(row) {
		return nil
	}
	return row[i]
}

var _ = fmt.Sprintf

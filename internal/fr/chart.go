package fr

import (
	"fmt"
	"strconv"
	"strings"
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
	case strings.Contains(cls, "column"):
		spec.Kind = "bar"
		if a := plot.Find("VanChartColumnPlotAttr"); a != nil && a.AttrBool("isBar", false) {
			spec.Horizontal = true
		}
	case strings.Contains(cls, "line"):
		spec.Kind = "line"
	case strings.Contains(cls, "area"):
		spec.Kind = "area"
	case strings.Contains(cls, "pie"):
		spec.Kind = "pie"
	case strings.Contains(cls, "funnel"):
		spec.Kind = "funnel"
	case strings.Contains(cls, "scatter"):
		spec.Kind = "scatter"
	case strings.Contains(cls, "bubble"):
		spec.Kind = "bar"
		unsupported = "气泡图已降级为柱形图"
	case strings.Contains(cls, "radar"):
		spec.Kind = "radar"
	case strings.Contains(cls, "gauge"):
		spec.Kind = "gauge"
	case strings.Contains(cls, "map"):
		spec.Kind = "bar"
		unsupported = "地图图表已降级为柱形图"
	case strings.Contains(cls, "custom"):
		spec.Kind = "bar"
		unsupported = "组合图表已降级为柱形图"
	case strings.Contains(cls, "multi"):
		spec.Kind = "bar"
		unsupported = "多层饼图已降级为柱形图"
	default:
		spec.Kind = "bar"
		if cls != "" {
			unsupported = "暂不支持的图表类型，已降级为柱形图"
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
	if spec.Kind == "pie" || spec.Kind == "funnel" {
		spec.Stack = false
	}
	spec.Colors = parseChartColors(plot)
	parseChartTitle(chart, spec)
	parseChartLegend(chart, spec)
	parseChartData(chart, spec, datasets, warn)
	if len(spec.Series) == 0 {
		warn("图表没有可用的数据系列")
	}
	if spec.Kind == "radar" && len(spec.Categories) < 3 {
		spec.Kind = "bar"
		warn("雷达图数据不足，已降级为柱形图")
	}
	return spec, warns
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

func parseChartData(chart *XNode, spec *ChartSpec, datasets map[string]*Dataset, warn func(string)) {
	def := chart.Child("ChartDefinition")
	if def == nil {
		def = chart.Find("ChartDefinition")
	}
	if def == nil {
		warn("图表缺少数据定义")
		return
	}
	var inner *XNode
	for _, n := range def.Children {
		if strings.Contains(n.Name, "Definition") {
			inner = n
			break
		}
	}
	if inner == nil {
		warn("图表数据定义无法识别")
		return
	}
	name := inner.Name
	if strings.Contains(name, "Map") || strings.Contains(name, "Dill") {
		warn("地图图表已降级为柱形图")
		if n := inner.Find("MoreNameCDDefinition"); n != nil {
			inner = n
		} else if n := inner.Find("OneValueCDDefinition"); n != nil {
			inner = n
		}
	}
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
		if nested := inner.Find("MoreNameCDDefinition"); nested != nil {
			name = nested.Name
			if td := nested.Child("TableData"); td != nil {
				if n := td.Child("Name"); n != nil {
					cd.dataset = datasets[n.TextTrim()]
				}
			}
			if cat := nested.Child("CategoryName"); cat != nil {
				cd.category = cat.Attr("value")
			}
			for _, sc := range nested.ChildAll("ChartSummaryColumn") {
				cd.valueCols = append(cd.valueCols, sc.Attr("name"))
				label := sc.Attr("customName")
				if label == "" {
					label = sc.Attr("name")
				}
				cd.valueNames = append(cd.valueNames, label)
			}
		} else if nested := inner.Find("OneValueCDDefinition"); nested != nil {
			name = nested.Name
			if td := nested.Child("TableData"); td != nil {
				if n := td.Child("Name"); n != nil {
					cd.dataset = datasets[n.TextTrim()]
				}
			}
			if cat := nested.Child("CategoryName"); cat != nil {
				cd.category = cat.Attr("value")
			}
			cd.seriesCol = nested.Attr("seriesName")
			cd.valueCol = nested.Attr("valueName")
		} else {
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

package fr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A chart definition may take its data from the report's own cells instead of
// a dataset. FineReport writes the reference as a formula:
//
//	<NormalReportDataDefinition>
//	  <Series><SeriesDefinition>
//	    <SeriesName><O class="com.fr.base.Formula"><Attributes>=C3</Attributes></O></SeriesName>
//	    <SeriesValue><O class="com.fr.base.Formula"><Attributes>=C4</Attributes></O></SeriesValue>
//	  </SeriesDefinition></Series>
//	  <Category><O class="com.fr.base.Formula"><Attributes>=B4</Attributes></O></Category>
//	</NormalReportDataDefinition>
//
// The referenced cells hold the values, so the chart has to be built after the
// sheet's cells have been evaluated. This file extracts those references.

// cellRefPattern matches a bare cell reference such as C3 or $B$4, but not a
// range (B2:B9) and not something that is part of a longer identifier.
var cellRefPattern = regexp.MustCompile(`^\$?([A-Za-z]{1,3})\$?([0-9]{1,7})$`)

// chartCellSource holds the cell references a chart definition asks for.
type chartCellSource struct {
	// seriesNames / seriesValues are parallel lists of cell references.
	seriesNames  []string
	seriesValues []string
	// category is the cell reference holding the category label.
	category string
	// single is true when the definition has no Series list, i.e. one value
	// cell and one category cell.
	singleValue string
}

// isReportRefDefinition reports whether the definition reads from report cells
// rather than from a dataset.
func isReportRefDefinition(name string) bool {
	switch {
	case strings.Contains(name, "NormalReportDataDefinition"):
		return true
	case strings.Contains(name, "ReportDataDefinition"):
		return true
	case strings.Contains(name, "BubbleReportDefinition"):
		return true
	case strings.Contains(name, "MeterReportDefinition"):
		return true
	}
	return false
}

// chartSourceOf returns the cell references a chart needs resolved.
func chartSourceOf(spec *ChartSpec) *chartCellSource {
	if spec == nil {
		return nil
	}
	return spec.cellSource
}

// parseChartCellSource extracts the cell references from a report-cell
// definition node.
func parseChartCellSource(n *XNode) *chartCellSource {
	if n == nil {
		return nil
	}
	src := &chartCellSource{}

	// <Series> holds one <SeriesDefinition> per series. The definition sits
	// one level down, so search the subtree rather than the direct children.
	for _, sd := range n.FindAll("SeriesDefinition") {
		name := formulaCellRef(sd.Child("SeriesName"))
		value := formulaCellRef(sd.Child("SeriesValue"))
		if name == "" && value == "" {
			// Some templates name the two children differently.
			value = formulaCellRef(sd.Child("Value"))
			name = formulaCellRef(sd.Child("Name"))
		}
		if name == "" && value == "" {
			continue
		}
		src.seriesNames = append(src.seriesNames, name)
		src.seriesValues = append(src.seriesValues, value)
	}
	src.category = formulaCellRef(n.Child("Category"))
	if src.category == "" {
		src.category = formulaCellRef(n.Child("CategoryName"))
	}
	if len(src.seriesValues) == 0 && src.category == "" {
		return nil
	}
	return src
}

// formulaCellRef reads a cell reference out of a formula node such as
// <O class="com.fr.base.Formula"><Attributes>=C3</Attributes></O>.
func formulaCellRef(n *XNode) string {
	if n == nil {
		return ""
	}
	// The formula may be nested one level deep in an <O> wrapper.
	attrs := n.Child("Attributes")
	text := ""
	if attrs != nil {
		text = attrs.TextTrim()
	} else {
		text = n.TextTrim()
	}
	if text == "" {
		// Try a nested <O> element.
		if o := n.Child("O"); o != nil {
			return formulaCellRef(o)
		}
		return ""
	}
	return normalizeCellRef(text)
}

// normalizeCellRef turns a formula such as "=C3" or "=C3+C4" into a cell
// reference when it is one, and returns "" otherwise.
func normalizeCellRef(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "=")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// A plain reference.
	if m := cellRefPattern.FindStringSubmatch(text); m != nil {
		return strings.ToUpper(m[1]) + m[2]
	}
	// A range: take the first cell so at least something sensible is plotted.
	if i := strings.Index(text, ":"); i > 0 {
		head := strings.TrimSpace(text[:i])
		if m := cellRefPattern.FindStringSubmatch(head); m != nil {
			return strings.ToUpper(m[1]) + m[2]
		}
	}
	return ""
}

// resolve reads the referenced cell values through the given lookup function.
// It returns the category labels, the per-series names and the per-series
// values.
//
// FineReport cell-chart bindings come in two shapes:
//   - a single value cell + a single category cell (one point), and
//   - an L-shaped matrix where a header row and a header column bracket a
//     rectangular value block. The latter must be expanded to the full grid.
func (src *chartCellSource) resolve(lookup func(string) (any, bool)) ([]string, []string, [][]any, error) {
	if src == nil {
		return nil, nil, nil, fmt.Errorf("没有可解析的单元格引用")
	}
	read := func(ref string) (any, bool) {
		if ref == "" {
			return nil, false
		}
		return lookup(ref)
	}

	// A single-value definition: one value cell, one category cell.
	if len(src.seriesValues) == 0 {
		v, ok := read(src.category)
		if !ok {
			return nil, nil, nil, fmt.Errorf("分类单元格 %s 取不到值", src.category)
		}
		return []string{fmt.Sprintf("%v", v)}, nil, [][]any{{v}}, nil
	}

	// L-shaped matrix binding: one SeriesDefinition together with a Category
	// header. FineReport expands the header anchors to the whole data region,
	// recovering the grouped bar/column chart instead of a single point.
	if len(src.seriesValues) == 1 && src.category != "" {
		if cats, names, series, err := src.resolveMatrix(read); err == nil && len(series) > 0 {
			return cats, names, series, nil
		}
	}

	// Legacy single-point handling: one plotted value per series definition.
	var cats []string
	var names []string
	var series [][]any
	for i, vref := range src.seriesValues {
		v, ok := read(vref)
		if !ok {
			// Skip the series whose value cell is empty; the report may have
			// a partially filled block.
			continue
		}
		name := ""
		if i < len(src.seriesNames) {
			if nv, ok := read(src.seriesNames[i]); ok {
				name = fmt.Sprintf("%v", nv)
			}
		}
		// Fall back to the value cell's own content so the series is not
		// labelled with a bare cell reference.
		if name == "" {
			if nv, ok := read(vref); ok {
				name = fmt.Sprintf("%v", nv)
			}
		}
		if name == "" {
			name = vref
		}
		names = append(names, name)
		series = append(series, []any{v})
	}
	// The category cell supplies the label for the single plotted point.
	if len(cats) == 0 {
		if cv, ok := read(src.category); ok {
			cats = append(cats, fmt.Sprintf("%v", cv))
		} else {
			cats = append(cats, "")
		}
	}
	return cats, names, series, nil
}

// resolveMatrix expands an L-shaped cell-chart binding. FineReport writes a
// single SeriesDefinition whose SeriesName and SeriesValue point at the header
// cells bracketing a value block, and a Category anchor on the other axis. We
// walk each header axis until the first empty cell to recover the full grid.
func (src *chartCellSource) resolveMatrix(read func(string) (any, bool)) ([]string, []string, [][]any, error) {
	nameRef := src.seriesNames[0]
	valRef := src.seriesValues[0]
	catRef := src.category

	nc, nr, okN := CellRefParts(nameRef)
	vc, vr, okV := CellRefParts(valRef)
	_, _, okC := CellRefParts(catRef)
	if !(okN && okV && okC) {
		return nil, nil, nil, fmt.Errorf("单元格引用无法解析")
	}

	// The two header axes are orthogonal: one runs horizontally (right), the
	// other vertically (down), meeting at the value corner. Detect which is
	// which from the L-shape relative to the value cell.
	seriesDown := false
	switch {
	case nc == vc:
		seriesDown = false // name shares the value's column -> horizontal header
	case nr == vr:
		seriesDown = true // name shares the value's row -> vertical header
	default:
		return nil, nil, nil, fmt.Errorf("系列方向与数值单元格不成 L 形")
	}
	catDown := !seriesDown

	seriesRefs := src.expandAxis(nameRef, seriesDown, read)
	catRefs := src.expandAxis(catRef, catDown, read)
	if len(seriesRefs) == 0 || len(catRefs) == 0 {
		return nil, nil, nil, fmt.Errorf("图表坐标轴扩展为空")
	}

	seriesNames := make([]string, len(seriesRefs))
	for j, sref := range seriesRefs {
		if nv, ok := read(sref); ok {
			if s := strings.TrimSpace(fmt.Sprintf("%v", nv)); s != "" {
				seriesNames[j] = s
				continue
			}
		}
		seriesNames[j] = sref
	}
	catNames := make([]string, len(catRefs))
	for i, cref := range catRefs {
		if cv, ok := read(cref); ok {
			catNames[i] = fmt.Sprintf("%v", cv)
		} else {
			catNames[i] = cref
		}
	}

	// series[j].data[i] = value at the block cell for (series j, category i).
	series := make([][]any, len(seriesRefs))
	for j := range seriesRefs {
		vals := make([]any, len(catRefs))
		for i := range catRefs {
			var col, row int
			if seriesDown {
				col, row = vc+i, vr+j
			} else {
				col, row = vc+j, vr+i
			}
			if v, ok := read(cellRefFromParts(col, row)); ok {
				vals[i] = v
			}
		}
		series[j] = vals
	}
	return catNames, seriesNames, series, nil
}

// expandAxis walks from anchor in the given direction collecting every
// non-empty cell reference until the first empty one. The anchor is included.
// This mirrors FineReport's cell-chart binding, where a single header cell such
// as =C3 marks the start of an entire axis.
func (src *chartCellSource) expandAxis(anchor string, down bool, read func(string) (any, bool)) []string {
	col, row, ok := CellRefParts(anchor)
	if !ok {
		return nil
	}
	var out []string
	for i := 0; i < 256; i++ {
		ref := cellRefFromParts(col, row)
		v, ok := read(ref)
		if !ok || isEmptyCell(v) {
			break
		}
		out = append(out, ref)
		if down {
			row++
		} else {
			col++
		}
	}
	return out
}

// isEmptyCell reports whether a resolved cell value should stop an axis walk.
func isEmptyCell(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// cellRefFromParts rebuilds an A1 reference from 0-based column/row parts.
func cellRefFromParts(col, row int) string {
	if col < 0 || row < 0 {
		return ""
	}
	var letters []byte
	c := col + 1
	for c > 0 {
		c--
		letters = append([]byte{byte('A' + c%26)}, letters...)
		c /= 26
	}
	return string(letters) + strconv.Itoa(row+1)
}

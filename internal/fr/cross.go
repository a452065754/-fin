package fr

import (
	"fmt"
	"strings"
)

// The cross renderer handles the cross-tab shape: one dataset column expands
// horizontally (the header band, Expand dir=1) while another expands
// vertically (the row band, Expand dir=0), and the cells where the two bands
// meet aggregate the dataset by the (row group, column value) pair.
//
// The plain vertical and horizontal renderers each ignore the other axis,
// which used to empty the cross cells: the header stayed one column wide, the
// body lost its per-column aggregates and every slice such as
// B2[;!0]A{B1="孙林"} resolved to nothing.

type crossRenderer struct {
	r  *renderState
	sh *SheetTemplate

	hCell *CellTemplate // the horizontal band cell (Expand dir=1)
	hCol  int
	hRow  int
	hVals []any // distinct values the band expands to

	vCells []*CellTemplate // vertical band cells (Expand dir=0)
	vRows  map[int]bool    // template rows that carry a vertical band cell

	groups []crossGroup
	ds     *Dataset // the dataset that drives both bands

	widths   []float64
	rows     []float64
	cells    []OutCell
	rawByOut map[[2]int]any

	crossMemo map[[3]int]any  // {cell index, group, item} -> aggregate
	fMemo     map[[2]int]any  // {cell index, group} -> formula value
	fBusy     map[[2]int]bool
	cellIdx   map[*CellTemplate]int
	vCellIdx  map[*CellTemplate]int

	pending []crossPending
}

type crossGroup struct {
	keys []any // one value per vertical band cell
	rows []int // dataset rows in the group
}

type crossPending struct {
	ct     *CellTemplate
	outRow int
	outCol int
	idx    int // index into x.cells
}

func (r *renderState) renderCross(sh *SheetTemplate) *Sheet {
	x := &crossRenderer{
		r:         r,
		sh:        sh,
		vRows:     map[int]bool{},
		rawByOut:  map[[2]int]any{},
		crossMemo: map[[3]int]any{},
		fMemo:     map[[2]int]any{},
		fBusy:     map[[2]int]bool{},
		cellIdx:   map[*CellTemplate]int{},
		vCellIdx:  map[*CellTemplate]int{},
	}
	x.collect()
	x.expandSize()
	x.emit()
	x.resolvePending()
	out := &Sheet{Name: sh.Name, Cols: x.widths, Rows: x.rows, Cells: x.cells}
	for _, w := range out.Cols {
		out.Width += w
	}
	return out
}

// collect picks the band cells and computes the horizontal values and the
// vertical groups.
func (x *crossRenderer) collect() {
	var hs []*CellTemplate
	for _, ct := range x.sh.CellList {
		if ct.Kind == "ds" && ct.ExpandDir == 1 {
			hs = append(hs, ct)
		}
	}
	if len(hs) == 0 {
		return
	}
	x.hCell = hs[0]
	for _, ct := range hs[1:] {
		if ct.R < x.hCell.R || (ct.R == x.hCell.R && ct.C < x.hCell.C) {
			x.hCell = ct
		}
	}
	x.hCol = x.hCell.C
	x.hRow = x.hCell.R
	for _, ct := range x.sh.CellList {
		if ct.Kind == "ds" && ct.ExpandDir == 0 {
			x.vCells = append(x.vCells, ct)
			x.vRows[ct.R] = true
		}
	}
	// Both bands normally share one dataset; the vertical one drives the
	// row grouping.
	x.ds = x.r.tpl.Datasets[x.hCell.DSName]
	if len(x.vCells) > 0 {
		if d := x.r.tpl.Datasets[x.vCells[0].DSName]; d != nil {
			x.ds = d
		}
	}
	if x.ds == nil || !x.ds.Loaded {
		return
	}
	// Horizontal values: distinct, first-seen order (FunctionGrouper).
	if hi := x.ds.ColumnIndex(x.hCell.Column); hi >= 0 {
		seen := map[string]bool{}
		for _, row := range x.ds.Rows {
			if hi >= len(row) {
				continue
			}
			k := stringOf(row[hi])
			if !seen[k] {
				seen[k] = true
				x.hVals = append(x.hVals, row[hi])
			}
		}
	}
	// Vertical groups: the tuple of the vertical band columns.
	groupIdx := map[string]int{}
	for i, row := range x.ds.Rows {
		var keys []any
		var kb strings.Builder
		for _, vc := range x.vCells {
			ci := x.ds.ColumnIndex(vc.Column)
			var v any
			if ci >= 0 && ci < len(row) {
				v = row[ci]
			}
			keys = append(keys, v)
			kb.WriteString(stringOf(v))
			kb.WriteString("\x00")
		}
		k := kb.String()
		gi, ok := groupIdx[k]
		if !ok {
			gi = len(x.groups)
			groupIdx[k] = gi
			x.groups = append(x.groups, crossGroup{keys: keys})
		}
		x.groups[gi].rows = append(x.groups[gi].rows, i)
	}
}

// expandSize lays out the output grid: the band column repeats per horizontal
// value and band rows repeat per vertical group.
func (x *crossRenderer) expandSize() {
	n := len(x.hVals)
	for c := 0; c < x.sh.MaxCol; c++ {
		w := x.sh.ColWidths[c]
		if c == x.hCol && n > 0 {
			for i := 0; i < n; i++ {
				x.widths = append(x.widths, w)
			}
		} else {
			x.widths = append(x.widths, w)
		}
	}
	ng := len(x.groups)
	for r := 0; r < x.sh.MaxRow; r++ {
		h := x.sh.RowHeights[r]
		if x.vRows[r] && ng > 0 {
			for i := 0; i < ng; i++ {
				x.rows = append(x.rows, h)
			}
		} else {
			x.rows = append(x.rows, h)
		}
	}
}

func (x *crossRenderer) mapCol(c int) int {
	if c > x.hCol && len(x.hVals) > 0 {
		return c + len(x.hVals) - 1
	}
	return c
}

// mapRow reports the first output row a template row lands on.
func (x *crossRenderer) mapRow(trow int) int {
	start := 0
	for r := 0; r < trow && r < x.sh.MaxRow; r++ {
		if x.vRows[r] {
			start += len(x.groups)
		} else {
			start++
		}
	}
	return start
}

func (x *crossRenderer) emit() {
	for trow := 0; trow < x.sh.MaxRow; trow++ {
		var rowCells []*CellTemplate
		for _, ct := range x.sh.CellList {
			if ct.R == trow {
				rowCells = append(rowCells, ct)
			}
		}
		if len(rowCells) == 0 {
			continue
		}
		base := x.mapRow(trow)
		switch {
		case trow == x.hRow:
			x.emitHeader(base, rowCells)
		case x.vRows[trow]:
			for gi := range x.groups {
				x.emitBody(base+gi, gi, rowCells)
			}
		default:
			x.emitStaticRow(base, rowCells)
		}
	}
}

// emitHeader writes the horizontal band row: the band cell repeats once per
// horizontal value and every other cell shifts right past the band.
func (x *crossRenderer) emitHeader(outRow int, rowCells []*CellTemplate) {
	for _, ct := range rowCells {
		if ct == x.hCell {
			for j, hv := range x.hVals {
				oc := OutCell{R: outRow, C: x.hCol + j, RS: ct.RS, CS: 1, Style: ct.Style}
				oc.Text = x.r.display(ct, hv)
				x.cells = append(x.cells, oc)
				x.rawByOut[[2]int{outRow, x.hCol + j}] = hv
			}
			continue
		}
		x.emitMapped(ct, outRow, x.staticRaw(ct))
	}
}

// emitBody writes one output row of the vertical band for group gi.
func (x *crossRenderer) emitBody(outRow, gi int, rowCells []*CellTemplate) {
	g := x.groups[gi]
	for _, ct := range rowCells {
		if ct.Kind == "ds" && ct.ExpandDir == 0 {
			// Vertical band cell: the group key.
			idx := x.vIndexOf(ct)
			var raw any
			if idx < len(g.keys) {
				raw = g.keys[idx]
			}
			x.emitMapped(ct, outRow, raw)
			continue
		}
		if ct.C == x.hCol && (ct.Kind == "ds" || ct.Kind == "formula") {
			// Cross cells: one per horizontal value.
			for j := range x.hVals {
				raw := x.crossRaw(ct, gi, j)
				oc := OutCell{R: outRow, C: x.hCol + j, RS: 1, CS: 1, Style: ct.Style, Widget: ct.Widget}
				oc.Text = x.r.display(ct, raw)
				x.cells = append(x.cells, oc)
				x.rawByOut[[2]int{outRow, x.hCol + j}] = raw
			}
			continue
		}
		x.emitMapped(ct, outRow, x.bodyRaw(ct, gi))
	}
}

// emitStaticRow writes a row that belongs to neither band. Formulas are
// deferred until the whole grid exists, because they may reference the
// expanded cells.
func (x *crossRenderer) emitStaticRow(outRow int, rowCells []*CellTemplate) {
	for _, ct := range rowCells {
		if ct.Kind == "formula" {
			oc := OutCell{R: outRow, C: x.mapCol(ct.C), RS: ct.RS, CS: ct.CS, Style: ct.Style, Widget: ct.Widget}
			x.cells = append(x.cells, oc)
			x.pending = append(x.pending, crossPending{ct: ct, outRow: outRow, outCol: x.mapCol(ct.C), idx: len(x.cells) - 1})
			continue
		}
		if ct.Kind == "chart" {
			oc := OutCell{R: outRow, C: x.mapCol(ct.C), RS: ct.RS, CS: ct.CS, Style: ct.Style, Widget: ct.Widget, Chart: x.r.chartFor(ct)}
			x.cells = append(x.cells, oc)
			continue
		}
		x.emitMapped(ct, outRow, x.staticRaw(ct))
	}
}

// resolvePending evaluates the deferred static-row formulas.
func (x *crossRenderer) resolvePending() {
	for _, p := range x.pending {
		ctx := NewEvalContext()
		ctx.Params = x.r.paramVal
		ctx.DatasetFields = x.r.datasetFields
		ctx.DatasetColumnValues = x.r.datasetColumnValues
		ctx.DatasetGroup = x.r.datasetGroup
		ctx.ReportCell = x.r.reportCell
		ctx.FormulaSQL = x.r.formulaSQL
		ctx.Ref = func(ref string) (any, bool) {
			col, row, ok := CellRefParts(ref)
			if !ok {
				return nil, false
			}
			return x.refRaw(row, col)
		}
		v, err := EvalFormula(p.ct.Formula, ctx)
		if err != nil {
			x.r.warn(fmt.Sprintf("公式 %s 计算失败: %v", strings.TrimSpace(p.ct.Formula), err))
			v = "#ERROR"
		}
		x.cells[p.idx].Text = x.r.display(p.ct, v)
		x.rawByOut[[2]int{p.outRow, p.outCol}] = v
	}
}

func (x *crossRenderer) emitMapped(ct *CellTemplate, outRow int, raw any) {
	oc := OutCell{R: outRow, C: x.mapCol(ct.C), RS: ct.RS, CS: ct.CS, Style: ct.Style, Widget: ct.Widget}
	if ct.Kind == "chart" {
		oc.Chart = x.r.chartFor(ct)
	} else {
		oc.Text = x.r.display(ct, raw)
	}
	x.cells = append(x.cells, oc)
	x.rawByOut[[2]int{outRow, x.mapCol(ct.C)}] = raw
}

// staticRaw evaluates a cell outside both bands.
func (x *crossRenderer) staticRaw(ct *CellTemplate) any {
	switch ct.Kind {
	case "ds":
		if x.ds != nil && x.ds.Loaded && len(x.ds.Rows) > 0 {
			return x.r.dsValue(ct, 0)
		}
		return nil
	case "text", "blank":
		return x.r.substitute(ct.Text)
	default:
		return ""
	}
}

// bodyRaw evaluates a non-band cell once per vertical group row.
func (x *crossRenderer) bodyRaw(ct *CellTemplate, gi int) any {
	switch ct.Kind {
	case "ds":
		rows := x.groups[gi].rows
		if len(rows) > 0 {
			return x.r.dsValue(ct, rows[0])
		}
		return nil
	case "formula":
		return x.evalCross(ct, gi)
	case "text", "blank":
		return x.r.substitute(ct.Text)
	default:
		return ""
	}
}

// evalCross evaluates a formula cell in the context of one vertical group
// row. References to the band column return the whole cross array so
// SUM(B2) aggregates across the horizontal expansion, and slices resolve
// against the cross grid.
func (x *crossRenderer) evalCross(ct *CellTemplate, gi int) any {
	idx := x.indexOf(ct)
	key := [2]int{idx, gi}
	if v, ok := x.fMemo[key]; ok {
		return v
	}
	if x.fBusy[key] {
		return nil
	}
	x.fBusy[key] = true
	defer delete(x.fBusy, key)

	g := x.groups[gi]
	ctx := NewEvalContext()
	ctx.Params = x.r.paramVal
	ctx.DatasetFields = x.r.datasetFields
	ctx.DatasetColumnValues = x.r.datasetColumnValues
	ctx.DatasetGroup = x.r.datasetGroup
	ctx.ReportCell = x.r.reportCell
	ctx.FormulaSQL = x.r.formulaSQL
	if len(g.rows) > 0 {
		ctx.CurRow = g.rows[0]
	}
	ctx.Seq = gi + 1
	ctx.Ref = func(ref string) (any, bool) {
		col, row, ok := CellRefParts(ref)
		if !ok {
			return nil, false
		}
		// A cell of the same body row is evaluated in this group's context.
		if x.vRows[row] && row == ct.R {
			tc := x.sh.Cells[[2]int{row, col}]
			if tc == nil {
				return nil, false
			}
			if tc.C == x.hCol && (tc.Kind == "ds" || tc.Kind == "formula") {
				return x.crossArray(tc, gi), true
			}
			return x.bodyRaw(tc, gi), true
		}
		// The horizontal band header reads as the array of its values.
		if row == x.hRow && col == x.hCol {
			return append([]any{}, x.hVals...), true
		}
		return x.refRaw(row, col)
	}
	ctx.Slice = func(cell, spec, condition string, curRow int) (any, error) {
		return x.sliceValues(cell, spec, condition, gi)
	}
	v, err := EvalFormula(ct.Formula, ctx)
	if err != nil {
		x.r.warn(fmt.Sprintf("公式 %s 计算失败: %v", strings.TrimSpace(ct.Formula), err))
		return "#ERROR"
	}
	x.fMemo[key] = v
	return v
}

// crossRaw computes the aggregate of one (vertical group, horizontal value)
// pair for a band-column body cell. A SummaryGrouper sums the column; a
// plain grouper takes the first matching value.
func (x *crossRenderer) crossRaw(ct *CellTemplate, gi, j int) any {
	idx := x.indexOf(ct)
	key := [3]int{idx, gi, j}
	if v, ok := x.crossMemo[key]; ok {
		return v
	}
		var raw any
	if x.ds != nil && x.ds.Loaded {
		ci := x.ds.ColumnIndex(ct.Column)
		hi := x.ds.ColumnIndex(x.hCell.Column)
		if ci >= 0 && hi >= 0 && ct.Column != x.hCell.Column {
			want := stringOf(x.hVals[j])
			if ct.RGSummary {
				sum := 0.0
				counted := false
				for _, r := range x.groups[gi].rows {
					row := x.ds.Rows[r]
					if hi >= len(row) || ci >= len(row) {
						continue
					}
					if stringOf(row[hi]) != want {
						continue
					}
					if f, ok := toFloat(row[ci]); ok {
						sum += f
						counted = true
					}
				}
				if counted {
					raw = sum
				} else {
					raw = 0.0
				}
			} else {
				for _, r := range x.groups[gi].rows {
					row := x.ds.Rows[r]
					if hi >= len(row) || ci >= len(row) {
						continue
					}
					if stringOf(row[hi]) != want {
						continue
					}
					raw = row[ci]
					break
				}
			}
		}
	}
	x.crossMemo[key] = raw
	return raw
}

// crossArray gathers every horizontal value of one band-column cell for a
// vertical group, in band order.
func (x *crossRenderer) crossArray(ct *CellTemplate, gi int) []any {
	out := make([]any, 0, len(x.hVals))
	for j := range x.hVals {
		out = append(out, x.crossRaw(ct, gi, j))
	}
	return out
}

// sliceValues resolves a slice against the cross grid. The horizontal form
// (empty row part, whole column direction) filters the band by the condition;
// the condition sees the band header value of each column.
func (x *crossRenderer) sliceValues(cell, spec, condition string, gi int) (any, error) {
	sp, err := parseSliceSpec(spec)
	if err != nil {
		return nil, err
	}
	colName, row, ok := CellRefParts(cell)
	if !ok {
		return nil, fmt.Errorf("切片引用 %q 无效", cell)
	}
	if sp.hasCol && !sp.hasRow {
		tc := x.sh.Cells[[2]int{row, colName}]
		if tc == nil {
			return nil, fmt.Errorf("切片引用的单元格 %s 不存在", cell)
		}
		var out []any
		for j, hv := range x.hVals {
			if condition != "" && !x.condHolds(condition, gi, j, hv) {
				continue
			}
			out = append(out, x.crossRaw(tc, gi, j))
		}
		if len(out) == 1 {
			return out[0], nil
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	}
	// A vertical slice over the group rows: the whole band area covers the
	// current group only, so B2[!0] yields the current row's cross array.
	if sp.hasRow && !sp.hasCol && sp.rowPart == "!0" {
		tc := x.sh.Cells[[2]int{row, colName}]
		if tc != nil && tc.C == x.hCol && x.vRows[row] {
			return x.crossArray(tc, gi), nil
		}
	}
	return nil, fmt.Errorf("交叉表中暂不支持切片 %s[%s]", cell, spec)
}

// condHolds evaluates a slice condition for one band column. The band header
// cell reads as that column's value; other same-row cells read through the
// group context.
func (x *crossRenderer) condHolds(cond string, gi, j int, hv any) bool {
	ctx := NewEvalContext()
	ctx.Params = x.r.paramVal
	ctx.Ref = func(ref string) (any, bool) {
		c, r, ok := CellRefParts(ref)
		if !ok {
			return nil, false
		}
		if r == x.hRow && c == x.hCol {
			return hv, true
		}
		return x.refRaw(r, c)
	}
	res, err := EvalFormula(cond, ctx)
	if err != nil {
		return false
	}
	return truthy(scalar(res))
}

// refRaw looks up an already emitted output value by template coordinates.
func (x *crossRenderer) refRaw(row, col int) (any, bool) {
	or := x.mapRow(row)
	if col == x.hCol && x.vRows[row] {
		var arr []any
		for j := 0; j < len(x.hVals); j++ {
			if v, ok := x.rawByOut[[2]int{or, x.hCol + j}]; ok {
				arr = append(arr, v)
			}
		}
		if len(arr) > 0 {
			return arr, true
		}
		return nil, false
	}
	if v, ok := x.rawByOut[[2]int{or, x.mapCol(col)}]; ok {
		return v, true
	}
	return nil, false
}

func (x *crossRenderer) indexOf(ct *CellTemplate) int {
	if i, ok := x.cellIdx[ct]; ok {
		return i
	}
	i := len(x.cellIdx)
	x.cellIdx[ct] = i
	return i
}

func (x *crossRenderer) vIndexOf(ct *CellTemplate) int {
	if i, ok := x.vCellIdx[ct]; ok {
		return i
	}
	i := len(x.vCellIdx)
	x.vCellIdx[ct] = i
	return i
}

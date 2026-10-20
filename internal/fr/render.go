package fr

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type renderState struct {
	tpl      *CPTTemplate
	dr       *DataResolver
	params   map[string]string
	paramVal map[string]any
	warnings []string
	warnSeen map[string]bool
}

// Render expands the template with dataset data and request parameters.
func (t *CPTTemplate) Render(dr *DataResolver, params map[string]string) *Preview {
	p := &Preview{Path: t.Path, Name: t.Name, Kind: "cpt", Title: t.Name, Params: t.Params}
	st := newRenderState(t, dr, params)
	st.loadDatasets()
	for _, sh := range t.Sheets {
		p.Sheets = append(p.Sheets, *st.renderSheet(sh))
	}
	p.Warnings = append(p.Warnings, st.warnings...)
	return p
}

func newRenderState(tpl *CPTTemplate, dr *DataResolver, params map[string]string) *renderState {
	st := &renderState{
		tpl:      tpl,
		dr:       dr,
		params:   params,
		paramVal: map[string]any{},
		warnSeen: map[string]bool{},
	}
	for _, pd := range tpl.Params {
		v := pd.Default
		if nv, ok := lookupParam(params, pd.Name); ok && nv != "" {
			v = nv
		}
		st.paramVal[pd.Name] = v
	}
	for k, v := range params {
		if _, ok := st.paramVal[k]; !ok {
			st.paramVal[k] = v
		}
	}
	return st
}

func (r *renderState) loadDatasets() {
	names := make([]string, 0, len(r.tpl.Datasets))
	for n := range r.tpl.Datasets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		ds := r.tpl.Datasets[n]
		for _, w := range ds.Warnings {
			r.warn(w)
		}
		if err := ds.Load(r.dr, r.params); err != nil {
			r.warn(fmt.Sprintf("数据集 %s: %v", n, err))
		}
	}
}

func lookupParam(params map[string]string, name string) (string, bool) {
	if v, ok := params[name]; ok {
		return v, true
	}
	for k, v := range params {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

func (r *renderState) warn(msg string) {
	if msg == "" || r.warnSeen[msg] {
		return
	}
	r.warnSeen[msg] = true
	r.warnings = append(r.warnings, msg)
}

func (r *renderState) renderSheet(sh *SheetTemplate) *Sheet {
	vCount, hCount := 0, 0
	for _, ct := range sh.CellList {
		if ct.Kind == "ds" && ct.ExpandDir == 0 {
			vCount++
		}
		if ct.Kind == "ds" && ct.ExpandDir == 1 {
			hCount++
		}
	}
	switch {
	case vCount > 0:
		return r.renderVertical(sh)
	case hCount > 0:
		return r.renderHorizontal(sh)
	default:
		return r.renderStatic(sh)
	}
}

// ---------- value helpers ----------

func (r *renderState) substitute(text string) string {
	if !strings.Contains(text, "${") {
		return text
	}
	out := text
	for k, v := range r.paramVal {
		out = strings.ReplaceAll(out, "${"+k+"}", stringOf(v))
	}
	return out
}

func (r *renderState) dictValue(ct *CellTemplate, raw any) (string, bool) {
	if ct.Dict == nil || raw == nil {
		return "", false
	}
	m, err := r.dr.Dictionary(ct.Dict.Conn, ct.Dict.Table, ct.Dict.KI, ct.Dict.VI)
	if err != nil {
		r.warn(fmt.Sprintf("字典 %s 读取失败: %v", ct.Dict.Table, err))
		return "", false
	}
	v, ok := m[stringOf(raw)]
	return v, ok
}

func (r *renderState) display(ct *CellTemplate, raw any) string {
	if v, ok := r.dictValue(ct, raw); ok {
		return v
	}
	if ct.Kind == "text" || ct.Kind == "blank" {
		return r.substitute(stringOf(raw))
	}
	return FormatValue(raw, ct.Style.NumberFormat)
}

func (r *renderState) chartFor(ct *CellTemplate) *ChartSpec {
	if ct.ChartXML == nil {
		return nil
	}
	spec, warns := ParseChartSpec(ct.ChartXML, r.tpl.Datasets)
	for _, w := range warns {
		r.warn(w)
	}
	return spec
}

func (r *renderState) dsValue(ct *CellTemplate, row int) any {
	ds := r.tpl.Datasets[ct.DSName]
	if ds == nil {
		r.warn("找不到数据集 " + ct.DSName)
		return nil
	}
	if !ds.Loaded {
		return nil
	}
	if row < 0 || row >= len(ds.Rows) {
		return nil
	}
	idx := ds.ColumnIndex(ct.Column)
	if idx < 0 || idx >= len(ds.Rows[row]) {
		r.warn(fmt.Sprintf("数据集 %s 中找不到列 %s", ds.Name, ct.Column))
		return nil
	}
	return ds.Rows[row][idx]
}

func (r *renderState) evalFormula(ct *CellTemplate, ref func(string) (any, bool), rng func(string, string) (any, bool), seq int) any {
	ctx := NewEvalContext()
	ctx.Params = r.paramVal
	ctx.Ref = ref
	ctx.Range = rng
	ctx.Seq = seq
	v, err := EvalFormula(ct.Formula, ctx)
	if err != nil {
		r.warn(fmt.Sprintf("公式 %s 计算失败: %v", strings.TrimSpace(ct.Formula), err))
		return "#ERROR"
	}
	return v
}

func compareRaw(a, b any) int {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	}
	return strings.Compare(stringOf(a), stringOf(b))
}

// ---------- static sheet ----------

func (r *renderState) renderStatic(sh *SheetTemplate) *Sheet {
	ev := &staticEval{r: r, sh: sh, memo: map[[2]int]any{}, busy: map[[2]int]bool{}}
	out := &Sheet{Name: sh.Name, Cols: append([]float64(nil), sh.ColWidths[:sh.MaxCol]...), Rows: append([]float64(nil), sh.RowHeights[:sh.MaxRow]...)}
	for _, ct := range sh.CellList {
		raw := ev.raw(ct)
		oc := OutCell{R: ct.R, C: ct.C, RS: ct.RS, CS: ct.CS, Style: ct.Style}
		if ct.Kind == "chart" {
			oc.Chart = r.chartFor(ct)
		} else {
			oc.Text = r.display(ct, raw)
		}
		out.Cells = append(out.Cells, oc)
	}
	for _, w := range out.Cols {
		out.Width += w
	}
	for _, h := range out.Rows {
		out.Height += h
	}
	return out
}

type staticEval struct {
	r    *renderState
	sh   *SheetTemplate
	memo map[[2]int]any
	busy map[[2]int]bool
}

func (e *staticEval) raw(ct *CellTemplate) any {
	if ct == nil {
		return nil
	}
	key := [2]int{ct.R, ct.C}
	if v, ok := e.memo[key]; ok {
		return v
	}
	if e.busy[key] {
		return nil
	}
	e.busy[key] = true
	defer delete(e.busy, key)
	var raw any
	switch ct.Kind {
	case "text", "blank":
		raw = e.r.substitute(ct.Text)
	case "ds":
		raw = e.r.dsValue(ct, 0)
	case "formula":
		raw = e.r.evalFormula(ct, e.ref, e.rng, 0)
	case "chart", "unsupported":
		raw = ""
	}
	e.memo[key] = raw
	return raw
}

func (e *staticEval) ref(ref string) (any, bool) {
	col, row, ok := CellRefParts(ref)
	if !ok {
		return nil, false
	}
	ct := e.sh.Cells[[2]int{row, col}]
	if ct == nil {
		return nil, false
	}
	return e.raw(ct), true
}

func (e *staticEval) rng(a, b string) (any, bool) {
	c1, r1, ok1 := CellRefParts(a)
	c2, r2, ok2 := CellRefParts(b)
	if !ok1 || !ok2 {
		return nil, false
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	var vals []any
	for row := r1; row <= r2; row++ {
		for col := c1; col <= c2; col++ {
			if ct := e.sh.Cells[[2]int{row, col}]; ct != nil {
				vals = append(vals, e.raw(ct))
			}
		}
	}
	return vals, true
}

// ---------- vertical expansion ----------

type bandInfo struct {
	row       int
	cells     []*CellTemplate
	ds        *Dataset
	groupCols []int
	details   []int
	summaries map[int][]int // level -> template row indexes
	postStart int
}

func (r *renderState) renderVertical(sh *SheetTemplate) *Sheet {
	band := r.findVerticalBand(sh)
	if band == nil || band.ds == nil {
		r.warn("未找到可用的纵向扩展数据区，按静态表格渲染")
		return r.renderStatic(sh)
	}
	bi := &vertRenderer{r: r, sh: sh, band: band, rawByOut: map[[2]int]any{}, bandMemo: map[[2]int]any{}, bandBusy: map[[2]int]bool{}}
	return bi.render()
}

func (r *renderState) findVerticalBand(sh *SheetTemplate) *bandInfo {
	rowCounts := map[int]int{}
	for _, ct := range sh.CellList {
		if ct.Kind == "ds" && ct.ExpandDir == 0 {
			rowCounts[ct.R]++
		}
	}
	if len(rowCounts) == 0 {
		return nil
	}
	bestRow, bestN := -1, 0
	for row, n := range rowCounts {
		if n > bestN || n == bestN && row < bestRow {
			bestRow, bestN = row, n
		}
	}
	if len(rowCounts) > 1 {
		r.warn("模板包含多个纵向扩展行，当前仅渲染第 " + strconv.Itoa(bestRow+1) + " 行")
	}
	band := &bandInfo{row: bestRow, summaries: map[int][]int{}}
	for _, ct := range sh.CellList {
		if ct.R == bestRow {
			band.cells = append(band.cells, ct)
		}
	}
	sort.Slice(band.cells, func(i, j int) bool { return band.cells[i].C < band.cells[j].C })
	// Dataset: use the first DS cell, prefer the one with the most rows.
	for _, ct := range band.cells {
		if ct.Kind == "ds" {
			if ds := r.tpl.Datasets[ct.DSName]; ds != nil && ds.Loaded && (band.ds == nil || len(ds.Rows) > len(band.ds.Rows)) {
				band.ds = ds
			}
		}
	}
	if band.ds == nil {
		for _, ct := range band.cells {
			if ct.Kind == "ds" {
				band.ds = r.tpl.Datasets[ct.DSName]
				break
			}
		}
	}
	if band.ds == nil {
		return band
	}
	n := len(band.ds.Rows)
	for i := 0; i < n; i++ {
		band.details = append(band.details, i)
	}
	// Group columns are the leading DS columns whose values repeat. This
	// mirrors FineReport's default grouping while keeping unique detail
	// columns (product names etc.) unmerged.
	dsCells := make([]*CellTemplate, 0, len(band.cells))
	for _, ct := range band.cells {
		if ct.Kind == "ds" && strings.EqualFold(ct.DSName, band.ds.Name) {
			dsCells = append(dsCells, ct)
		}
	}
	for _, ct := range dsCells {
		if ct.HasRG && ct.RGMode != 0 {
			break
		}
		vals := map[string]bool{}
		for _, row := range band.details {
			vals[stringOf(r.dsValue(ct, row))] = true
		}
		if n > 1 && len(vals) >= n {
			break
		}
		if !ct.HasRG {
			break
		}
		band.groupCols = append(band.groupCols, ct.C)
	}
	if len(band.groupCols) > 0 && len(band.groupCols) < len(dsCells) {
		// A unique first detail column means the remaining columns are details.
	}
	// Sort detail rows by group key, matching FineReport's grouping order.
	if len(band.groupCols) > 0 {
		sort.SliceStable(band.details, func(i, j int) bool {
			for _, col := range band.groupCols {
				ct := cellAtCol(band.cells, col)
				if ct == nil {
					continue
				}
				c := compareRaw(r.dsValue(ct, band.details[i]), r.dsValue(ct, band.details[j]))
				if c != 0 {
					return c < 0
				}
			}
			return false
		})
	}
	// Summary rows are the rows right below the band covered by a spanning
	// cell of the band. Their group level is the deepest covering group cell.
	maxSpan := 1
	for _, ct := range band.cells {
		if ct.RS > maxSpan {
			maxSpan = ct.RS
		}
	}
	last := band.row
	for s := band.row + 1; s < band.row+maxSpan && s < sh.MaxRow; s++ {
		covered := false
		level := -1
		for _, ct := range band.cells {
			if s < band.row+ct.RS {
				covered = true
				for gi, gc := range band.groupCols {
					if gc == ct.C && gi > level {
						level = gi
					}
				}
			}
		}
		if !covered {
			break
		}
		band.summaries[level] = append(band.summaries[level], s)
		last = s
	}
	band.postStart = last + 1
	return band
}

func cellAtCol(cells []*CellTemplate, col int) *CellTemplate {
	for _, ct := range cells {
		if ct.C == col {
			return ct
		}
	}
	return nil
}

type vertRenderer struct {
	r        *renderState
	sh       *SheetTemplate
	band     *bandInfo
	cells    []OutCell
	rows     []float64
	rawByOut map[[2]int]any
	bandMemo map[[2]int]any
	bandBusy map[[2]int]bool
}

func (v *vertRenderer) render() *Sheet {
	// Static rows above the band.
	var above []int
	for r := 0; r < v.band.row; r++ {
		above = append(above, r)
	}
	for _, row := range above {
		v.emitTemplateRow(row, v.band.cells, nil, nil)
	}
	if len(v.band.groupCols) == 0 {
		for _, d := range v.band.details {
			v.emitDetail(d)
		}
		if sums := v.band.summaries[-1]; len(sums) > 0 {
			for _, s := range sums {
				v.emitTemplateRow(s, v.band.cells, v.band.details, nil)
			}
		}
	} else {
		v.emitLevel(0, v.band.details)
		if sums := v.band.summaries[-1]; len(sums) > 0 {
			for _, s := range sums {
				v.emitTemplateRow(s, v.band.cells, v.band.details, nil)
			}
		}
	}
	for _, row := range v.collectPostRows() {
		v.emitTemplateRow(row, v.band.cells, v.band.details, nil)
	}
	out := &Sheet{Name: v.sh.Name, Cols: append([]float64(nil), v.sh.ColWidths[:v.sh.MaxCol]...), Rows: v.rows}
	sort.SliceStable(v.cells, func(i, j int) bool {
		if v.cells[i].R != v.cells[j].R {
			return v.cells[i].R < v.cells[j].R
		}
		return v.cells[i].C < v.cells[j].C
	})
	out.Cells = v.cells
	for _, w := range out.Cols {
		out.Width += w
	}
	for _, h := range out.Rows {
		out.Height += h
	}
	return out
}

func (v *vertRenderer) collectPostRows() []int {
	var out []int
	for r := v.band.postStart; r < v.sh.MaxRow; r++ {
		out = append(out, r)
	}
	return out
}

func (v *vertRenderer) coveredCols(summaryRow int) map[int]bool {
	cov := map[int]bool{}
	for _, ct := range v.band.cells {
		if ct.Kind == "ds" && v.isGroupCol(ct.C) && summaryRow >= v.band.row && summaryRow < v.band.row+ct.RS {
			for c := ct.C; c < ct.C+ct.CS; c++ {
				cov[c] = true
			}
		}
	}
	return cov
}

func (v *vertRenderer) isGroupCol(col int) bool {
	for _, c := range v.band.groupCols {
		if c == col {
			return true
		}
	}
	return false
}

func (v *vertRenderer) emitLevel(level int, rows []int) {
	if len(rows) == 0 {
		return
	}
	cols := v.band.groupCols
	if level >= len(cols) {
		for _, d := range rows {
			v.emitDetail(d)
		}
		return
	}
	partitions := partitionRows(rows, func(a, b int) int {
		ct := cellAtCol(v.band.cells, cols[level])
		if ct == nil {
			return 0
		}
		return compareRaw(v.r.dsValue(ct, a), v.r.dsValue(ct, b))
	})
	for _, part := range partitions {
		start := len(v.rows)
		if level+1 >= len(cols) {
			for _, d := range part {
				v.emitDetail(d)
			}
		} else {
			v.emitLevel(level+1, part)
		}
		for _, s := range v.band.summaries[level] {
			v.emitTemplateRow(s, v.band.cells, part, nil)
		}
		end := len(v.rows)
		v.emitGroupCell(cols[level], part, start, end)
	}
}

func partitionRows(rows []int, cmp func(a, b int) int) [][]int {
	var parts [][]int
	for _, row := range rows {
		if len(parts) == 0 || cmp(parts[len(parts)-1][0], row) != 0 {
			parts = append(parts, []int{row})
		} else {
			parts[len(parts)-1] = append(parts[len(parts)-1], row)
		}
	}
	return parts
}

func (v *vertRenderer) emitGroupCell(col int, rows []int, start, end int) {
	ct := cellAtCol(v.band.cells, col)
	if ct == nil || end <= start {
		return
	}
	var raw any
	if len(rows) > 0 {
		raw = v.r.dsValue(ct, rows[0])
	}
	oc := OutCell{R: start, C: ct.C, RS: end - start, CS: ct.CS, Style: ct.Style, Text: v.r.display(ct, raw)}
	v.cells = append(v.cells, oc)
	if len(rows) > 0 {
		v.rawByOut[[2]int{start, col}] = v.r.dsValue(ct, rows[0])
	}
}

func (v *vertRenderer) emitDetail(d int) {
	outRow := len(v.rows)
	v.rows = append(v.rows, v.sh.RowHeights[v.band.row])
	for _, ct := range v.band.cells {
		if v.isGroupCol(ct.C) {
			continue
		}
		raw := v.bandCellRaw(ct, d)
		oc := OutCell{R: outRow, C: ct.C, RS: 1, CS: ct.CS, Style: ct.Style}
		if ct.Kind == "chart" {
			oc.Chart = v.r.chartFor(ct)
		} else {
			oc.Text = v.r.display(ct, raw)
		}
		v.cells = append(v.cells, oc)
		v.rawByOut[[2]int{outRow, ct.C}] = raw
	}
}

// bandCellRaw evaluates a band template cell for one dataset row.
func (v *vertRenderer) bandCellRaw(ct *CellTemplate, d int) any {
	key := [2]int{d, ct.C}
	if val, ok := v.bandMemo[key]; ok {
		return val
	}
	if v.bandBusy[key] {
		return nil
	}
	v.bandBusy[key] = true
	defer delete(v.bandBusy, key)
	var raw any
	switch ct.Kind {
	case "ds":
		raw = v.r.dsValue(ct, d)
	case "formula":
		raw = v.r.evalFormula(ct, func(ref string) (any, bool) {
			col, row, ok := CellRefParts(ref)
			if !ok {
				return nil, false
			}
			if row == v.band.row {
				tc := cellAtCol(v.band.cells, col)
				if tc == nil {
					return nil, false
				}
				return v.bandCellRaw(tc, d), true
			}
			return v.outRef(row, col)
		}, func(a, b string) (any, bool) {
			return v.bandRange(a, b, []int{d})
		}, d+1)
	case "text", "blank":
		raw = v.r.substitute(ct.Text)
	default:
		raw = ""
	}
	v.bandMemo[key] = raw
	return raw
}

func (v *vertRenderer) outRef(row, col int) (any, bool) {
	key := [2]int{v.lastOutRowFor(row), col}
	if val, ok := v.rawByOut[key]; ok {
		return val, true
	}
	return nil, false
}

func (v *vertRenderer) lastOutRowFor(templateRow int) int {
	// Walk emitted rows backwards to approximate template-row mapping.
	for i := len(v.rows) - 1; i >= 0; i-- {
		if i < v.band.row {
			return i
		}
	}
	return -1
}

func (v *vertRenderer) bandRange(a, b string, scope []int) (any, bool) {
	c1, r1, ok1 := CellRefParts(a)
	c2, r2, ok2 := CellRefParts(b)
	if !ok1 || !ok2 {
		return nil, false
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	var vals []any
	for d := range scope {
		_ = d
	}
	for _, d := range scope {
		for row := r1; row <= r2; row++ {
			if row != v.band.row {
				continue
			}
			for col := c1; col <= c2; col++ {
				if ct := cellAtCol(v.band.cells, col); ct != nil {
					vals = append(vals, v.bandCellRaw(ct, d))
				}
			}
		}
	}
	return vals, true
}

// emitTemplateRow renders a non-band template row (header, summary, note).
func (v *vertRenderer) emitTemplateRow(trow int, bandCells []*CellTemplate, scope []int, _ map[int]bool) {
	outRow := len(v.rows)
	v.rows = append(v.rows, v.sh.RowHeights[trow])
	covered := v.coveredCols(trow)
	for _, ct := range v.sh.CellList {
		if ct.R != trow {
			continue
		}
		if covered[ct.C] {
			continue
		}
		raw := v.staticRaw(ct, scope)
		oc := OutCell{R: outRow, C: ct.C, RS: ct.RS, CS: ct.CS, Style: ct.Style}
		if ct.Kind == "chart" {
			oc.Chart = v.r.chartFor(ct)
		} else {
			oc.Text = v.r.display(ct, raw)
		}
		v.cells = append(v.cells, oc)
		v.rawByOut[[2]int{outRow, ct.C}] = raw
	}
}

func (v *vertRenderer) staticRaw(ct *CellTemplate, scope []int) any {
	switch ct.Kind {
	case "text", "blank":
		return v.r.substitute(ct.Text)
	case "formula":
		if scope == nil {
			scope = v.band.details
		}
		return v.r.evalFormula(ct, func(ref string) (any, bool) {
			col, row, ok := CellRefParts(ref)
			if !ok {
				return nil, false
			}
			if row == v.band.row {
				if tc := cellAtCol(v.band.cells, col); tc != nil {
					vals := make([]any, 0, len(scope))
					for _, d := range scope {
						vals = append(vals, v.bandCellRaw(tc, d))
					}
					if len(vals) == 1 {
						return vals[0], true
					}
					return vals, true
				}
			}
			// Look up an already rendered cell (same summary row first).
			for i := len(v.rows) - 1; i >= 0; i-- {
				if val, ok := v.rawByOut[[2]int{i, col}]; ok && row < v.band.row {
					return val, true
				}
			}
			return nil, false
		}, func(a, b string) (any, bool) {
			return v.bandRange(a, b, scope)
		}, 0)
	case "ds":
		if len(scope) > 0 {
			return v.r.dsValue(ct, scope[0])
		}
		return nil
	default:
		return ""
	}
}

// ---------- horizontal expansion ----------

func (r *renderState) renderHorizontal(sh *SheetTemplate) *Sheet {
	hr := &horizRenderer{r: r, sh: sh, rawByOut: map[[2]int]any{}, staticMemo: map[[2]int]any{}, staticBusy: map[[2]int]bool{}, dynMemo: map[[2]int]any{}, dynBusy: map[[2]int]bool{}}
	return hr.render()
}

type horizRenderer struct {
	r          *renderState
	sh         *SheetTemplate
	bandCol    int
	bandRows   map[int]bool
	dynCells   []*CellTemplate
	ds         *Dataset
	items      int
	widths     []float64
	rows       []float64
	cells      []OutCell
	rawByOut   map[[2]int]any
	staticMemo map[[2]int]any
	staticBusy map[[2]int]bool
	dynMemo    map[[2]int]any
	dynBusy    map[[2]int]bool
}

func (h *horizRenderer) render() *Sheet {
	colCount := map[int]int{}
	for _, ct := range h.sh.CellList {
		if ct.Kind == "ds" && ct.ExpandDir == 1 {
			colCount[ct.C]++
		}
	}
	if len(colCount) == 0 {
		return h.r.renderStatic(h.sh)
	}
	bestCol, bestN := -1, 0
	for col, n := range colCount {
		if n > bestN || n == bestN && col < bestCol {
			bestCol, bestN = col, n
		}
	}
	if len(colCount) > 1 {
		h.r.warn("模板包含多个横向扩展列，当前仅渲染第 " + strconv.Itoa(bestCol+1) + " 列")
	}
	h.bandCol = bestCol
	h.bandRows = map[int]bool{}
	for _, ct := range h.sh.CellList {
		if ct.C == h.bandCol && ct.ExpandDir == 1 && (ct.Kind == "ds" || ct.Kind == "formula" || ct.Kind == "chart") {
			h.bandRows[ct.R] = true
			h.dynCells = append(h.dynCells, ct)
		}
	}
	for _, ct := range h.sh.CellList {
		if ct.Kind == "ds" && ct.C == h.bandCol {
			if ds := h.r.tpl.Datasets[ct.DSName]; ds != nil && (h.ds == nil || len(ds.Rows) > len(h.ds.Rows)) {
				h.ds = ds
			}
		}
	}
	if h.ds != nil && h.ds.Loaded {
		h.items = len(h.ds.Rows)
	}
	// Compute output column widths: the band column is repeated per item.
	for c := 0; c < h.sh.MaxCol; c++ {
		w := h.sh.ColWidths[c]
		if c == h.bandCol {
			for i := 0; i < h.items; i++ {
				h.widths = append(h.widths, w)
			}
			if h.items == 0 {
				// Keep the static label layout intact when there is no data.
				h.widths = append(h.widths, 0)
			}
		} else {
			h.widths = append(h.widths, w)
		}
	}
	h.rows = append(h.rows, h.sh.RowHeights[:h.sh.MaxRow]...)
	// Emit rows in template order.
	for row := 0; row < h.sh.MaxRow; row++ {
		for _, ct := range h.sh.CellList {
			if ct.R != row {
				continue
			}
			isDyn := ct.C == h.bandCol && h.bandRows[ct.R] && (ct.Kind == "ds" || ct.Kind == "formula" || ct.Kind == "chart")
			if isDyn {
				for i := 0; i < h.items; i++ {
					raw := h.dynRaw(ct, i)
					outC := h.bandCol + i
					oc := OutCell{R: row, C: outC, RS: ct.RS, CS: ct.CS, Style: ct.Style}
					if ct.Kind == "chart" {
						oc.Chart = h.r.chartFor(ct)
					} else {
						oc.Text = h.r.display(ct, raw)
					}
					h.cells = append(h.cells, oc)
					h.rawByOut[[2]int{row, outC}] = raw
				}
				continue
			}
			outC, outCS := h.mapCol(ct.C, ct.CS)
			raw := h.staticRaw(ct)
			oc := OutCell{R: row, C: outC, RS: ct.RS, CS: outCS, Style: ct.Style}
			if ct.Kind == "chart" {
				oc.Chart = h.r.chartFor(ct)
			} else {
				oc.Text = h.r.display(ct, raw)
			}
			h.cells = append(h.cells, oc)
			h.rawByOut[[2]int{row, outC}] = raw
		}
	}
	out := &Sheet{Name: h.sh.Name}
	// Trim trailing zero-width band placeholder if there is no data.
	out.Cols = h.widths
	out.Rows = h.rows
	out.Cells = h.cells
	for _, w := range out.Cols {
		out.Width += w
	}
	for _, hh := range out.Rows {
		out.Height += hh
	}
	return out
}

func (h *horizRenderer) mapCol(c, cs int) (int, int) {
	if c+cs <= h.bandCol {
		return c, cs
	}
	if c <= h.bandCol && c+cs > h.bandCol {
		n := cs + h.items - 1
		if n < 1 {
			n = 1
		}
		return c, n
	}
	return c + h.items - 1, cs
}

func (h *horizRenderer) dynRaw(ct *CellTemplate, i int) any {
	key := [2]int{i, ct.R}
	if val, ok := h.dynMemo[key]; ok {
		return val
	}
	if h.dynBusy[key] {
		return nil
	}
	h.dynBusy[key] = true
	defer delete(h.dynBusy, key)
	var raw any
	switch ct.Kind {
	case "ds":
		raw = h.r.dsValue(ct, i)
	case "formula":
		raw = h.r.evalFormula(ct, func(ref string) (any, bool) {
			col, row, ok := CellRefParts(ref)
			if !ok {
				return nil, false
			}
			if col == h.bandCol && h.bandRows[row] {
				for _, tc := range h.dynCells {
					if tc.R == row {
						return h.dynRaw(tc, i), true
					}
				}
			}
			if tc := h.sh.Cells[[2]int{row, col}]; tc != nil {
				return h.staticRaw(tc), true
			}
			return nil, false
		}, func(a, b string) (any, bool) {
			return h.rangeVals(a, b)
		}, i+1)
	case "text", "blank":
		raw = h.r.substitute(ct.Text)
	default:
		raw = ""
	}
	h.dynMemo[key] = raw
	return raw
}

func (h *horizRenderer) staticRaw(ct *CellTemplate) any {
	key := [2]int{ct.R, ct.C}
	if val, ok := h.staticMemo[key]; ok {
		return val
	}
	if h.staticBusy[key] {
		return nil
	}
	h.staticBusy[key] = true
	defer delete(h.staticBusy, key)
	var raw any
	switch ct.Kind {
	case "text", "blank":
		raw = h.r.substitute(ct.Text)
	case "formula":
		raw = h.r.evalFormula(ct, func(ref string) (any, bool) {
			col, row, ok := CellRefParts(ref)
			if !ok {
				return nil, false
			}
			if col == h.bandCol && h.bandRows[row] {
				vals := make([]any, 0, h.items)
				for _, tc := range h.dynCells {
					if tc.R == row {
						for i := 0; i < h.items; i++ {
							vals = append(vals, h.dynRaw(tc, i))
						}
						return vals, true
					}
				}
			}
			if tc := h.sh.Cells[[2]int{row, col}]; tc != nil {
				return h.staticRaw(tc), true
			}
			return nil, false
		}, func(a, b string) (any, bool) {
			return h.rangeVals(a, b)
		}, 0)
	case "ds":
		raw = h.r.dsValue(ct, 0)
	default:
		raw = ""
	}
	h.staticMemo[key] = raw
	return raw
}

func (h *horizRenderer) rangeVals(a, b string) (any, bool) {
	c1, r1, ok1 := CellRefParts(a)
	c2, r2, ok2 := CellRefParts(b)
	if !ok1 || !ok2 {
		return nil, false
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	var vals []any
	for row := r1; row <= r2; row++ {
		for col := c1; col <= c2; col++ {
			if ct := h.sh.Cells[[2]int{row, col}]; ct != nil {
				vals = append(vals, h.staticRaw(ct))
			}
		}
	}
	return vals, true
}

// FirstRow returns the first non-empty cell text of a sheet, used as a title
// fallback for FVS blocks and previews.
func (sh *SheetTemplate) FirstRow() string {
	for _, ct := range sh.CellList {
		if ct.Kind == "text" && strings.TrimSpace(ct.Text) != "" {
			return strings.TrimSpace(ct.Text)
		}
	}
	return ""
}

var _ = fmt.Sprintf

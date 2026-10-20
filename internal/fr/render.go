package fr

import (
	"fmt"
	"os"
	"path/filepath"
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

	// cellValues holds the evaluated value of every report cell, keyed by
	// sheet index then cell coordinates. Charts whose data definition points
	// at report cells (rather than a dataset) read from here.
	cellValues map[int]map[[2]int]any
	// renderedSheets keeps the sheets rendered so far so that a formula can
	// reference another sheet of the same report.
	renderedSheets []Sheet
	// hasForm reports whether any rendered cell carried a fill-in control.
	hasForm bool
	// sheetIndex is the index of the sheet currently being rendered.
	sheetIndex int
}

// Render expands the template with dataset data and request parameters.
func (t *CPTTemplate) Render(dr *DataResolver, params map[string]string) *Preview {
	p := &Preview{Path: t.Path, Name: t.Name, Kind: "cpt", Title: t.Name, Params: t.Params}
	st := newRenderState(t, dr, params)
	st.loadDatasets()
	// Sheets are rendered up front so that cross-sheet references such as
	// '2010-实际'!N5 can read the referenced sheet's cells.
	sheets := make([]Sheet, len(t.Sheets))
	for i, sh := range t.Sheets {
		st.sheetIndex = i
		sheets[i] = *st.renderSheet(sh)
		// Keep the finished sheet so a later sheet can reference it.
		st.renderedSheets = append(st.renderedSheets, sheets[i])
	}
	p.Sheets = sheets
	p.H5 = t.H5
	p.HasForm = st.hasForm
	p.Warnings = append(p.Warnings, st.warnings...)
	return p
}

func newRenderState(tpl *CPTTemplate, dr *DataResolver, params map[string]string) *renderState {
	st := &renderState{
		tpl:        tpl,
		dr:         dr,
		params:     params,
		paramVal:   map[string]any{},
		warnSeen:   map[string]bool{},
		cellValues: map[int]map[[2]int]any{},
		sheetIndex: -1,
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
	var out *Sheet
	switch {
	case vCount > 0:
		out = r.renderVertical(sh)
	case hCount > 0:
		out = r.renderHorizontal(sh)
	default:
		out = r.renderStatic(sh)
	}
	// Charts that read from report cells need the rendered cell values, so
	// they are resolved in a second pass once the grid is known.
	r.resolveCellCharts(sh, out)
	// Note whether the sheet carries fill-in controls.
	for _, c := range out.Cells {
		if c.Widget != nil {
			r.hasForm = true
			break
		}
	}
	return out
}

// resolveCellCharts fills in charts whose data definition references report
// cells instead of a dataset.
func (r *renderState) resolveCellCharts(sh *SheetTemplate, out *Sheet) {
	if out == nil {
		return
	}
	// Collect the charts that need report-cell data.
	var pending []*OutCell
	for i := range out.Cells {
		oc := &out.Cells[i]
		if oc.Chart != nil && oc.Chart.NeedsCellSource {
			pending = append(pending, oc)
		}
	}
	if len(pending) == 0 {
		return
	}
	// Use the rendered grid values so that references like =C3 point at the
	// expanded output, which is what the template author sees.
	values := r.outputCellValues(out)
	for _, oc := range pending {
		src := chartSourceOf(oc.Chart)
		if src == nil {
			continue
		}
		cats, names, series, err := src.resolve(func(ref string) (any, bool) {
			col, row, ok := CellRefParts(ref)
			if !ok {
				return nil, false
			}
			v, ok := values[[2]int{row, col}]
			return v, ok
		})
		if err != nil {
			r.warn("图表引用的报表单元格取不到值：" + err.Error())
			continue
		}
		oc.Chart.Categories = cats
		oc.Chart.Series = nil
		for i, vals := range series {
			name := ""
			if i < len(names) {
				name = names[i]
			}
			data := make([]float64, 0, len(vals))
			for _, v := range vals {
				if f, ok := toFloat(v); ok {
					data = append(data, f)
				}
			}
			oc.Chart.Series = append(oc.Chart.Series, ChartSeries{
				Name: name,
				Data: data,
				Type: oc.Chart.Kind,
			})
		}
		oc.Chart.NeedsCellSource = false
	}
}

// outputCellValues maps the rendered grid back to cell values so that a
// chart's cell reference (e.g. =C3) can be resolved. The output grid's text
// is the display form; numbers keep their numeric meaning when possible.
func (r *renderState) outputCellValues(out *Sheet) map[[2]int]any {
	values := map[[2]int]any{}
	for _, oc := range out.Cells {
		if oc.Chart != nil {
			continue
		}
		values[[2]int{oc.R, oc.C}] = oc.Text
	}
	return values
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
	s := FormatValue(raw, ct.Style.NumberFormat)
	// Fill-in controls store their value with the report's own line breaks
	// in front of it; the control itself shows the bare value.
	if ct.Widget != nil {
		s = strings.TrimLeft(s, "\r\n\t ")
	}
	return s
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
	ctx.ReportCell = r.reportCell
	ctx.SheetCell = r.sheetCell
	ctx.DatasetFields = r.datasetFields
	ctx.DatasetColumnValues = r.datasetColumnValues
	ctx.DatasetGroup = r.datasetGroup
	v, err := EvalFormula(ct.Formula, ctx)
	if err != nil {
		r.warn(fmt.Sprintf("公式 %s 计算失败: %v", strings.TrimSpace(ct.Formula), err))
		return "#ERROR"
	}
	return v
}

// reportCell resolves FineReport's cross-report reference
// VALUE("report name", row, column). The referenced report is looked up by
// its file name under the reportlets tree and rendered on demand.
func (r *renderState) reportCell(name string, row, col int) (any, bool) {
	if r.dr == nil || r.dr.Webroot == "" {
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	path, ok := r.findReportByName(name)
	if !ok {
		return nil, false
	}
	preview, err := r.renderOtherReport(path)
	if err != nil {
		r.warn(fmt.Sprintf("公式引用报表 %s 失败: %v", name, err))
		return nil, false
	}
	if len(preview.Sheets) == 0 {
		return nil, false
	}
	sh := preview.Sheets[0]
	for _, c := range sh.Cells {
		if c.R == row-1 && c.C == col-1 {
			return c.Text, true
		}
	}
	return nil, false
}

// findReportByName searches the reportlets tree for a report whose file name
// matches the given name (without the .cpt extension).
func (r *renderState) findReportByName(name string) (string, bool) {
	root := filepath.Join(r.dr.Webroot, "WEB-INF", "reportlets")
	// Direct hit first: the report may live next to the current template.
	if p, ok := r.findInDir(root, name); ok {
		return p, true
	}
	return "", false
}

// findInDir walks one directory level looking for name.cpt / name.fvs.
func (r *renderState) findInDir(dir, name string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if base == name {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}

// renderOtherReport parses and renders a report that a formula referenced.
func (r *renderState) renderOtherReport(path string) (*Preview, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".cpt":
		tpl, err := ParseCPTFile(path)
		if err != nil {
			return nil, err
		}
		return tpl.Render(r.dr, r.params), nil
	case ".fvs":
		tpl, err := ParseFVSFile(path)
		if err != nil {
			return nil, err
		}
		return tpl.Render(r.dr, r.params), nil
	}
	return nil, fmt.Errorf("不支持的报表类型 %s", ext)
}

// datasetFields lists the column names of a dataset declared in the template.
func (r *renderState) datasetFields(name string) ([]string, bool) {
	ds, ok := r.tpl.Datasets[name]
	if !ok {
		return nil, false
	}
	if len(ds.Columns) > 0 {
		return ds.Columns, true
	}
	return nil, false
}

// datasetColumnValues returns every value of one dataset column. It backs
// FineReport's dataset.select(column) method.
func (r *renderState) datasetColumnValues(dataset, column string) ([]any, bool) {
	ds, ok := r.tpl.Datasets[dataset]
	if !ok || !ds.Loaded {
		return nil, false
	}
	idx := ds.ColumnIndex(column)
	if idx < 0 {
		return nil, false
	}
	out := make([]any, 0, len(ds.Rows))
	for _, row := range ds.Rows {
		if idx < len(row) {
			out = append(out, row[idx])
			continue
		}
		out = append(out, nil)
	}
	return out, true
}

// datasetGroup returns the aggregated value of a dataset group. It backs
// FineReport's dataset.group(key) method, which reports the value of the
// group the current cell belongs to.
func (r *renderState) datasetGroup(dataset string, key any) (any, bool) {
	ds, ok := r.tpl.Datasets[dataset]
	if !ok || !ds.Loaded {
		return nil, false
	}
	// Without the group layout the value is the single cell the group
	// points at, which is the common case in a formula.
	return key, true
}

// sheetCell resolves a cross-sheet reference such as '2010-实际'!N5 by
// looking the cell up in the already rendered sheets.
func (r *renderState) sheetCell(sheet, ref string) (any, bool) {
	col, row, ok := CellRefParts(ref)
	if !ok {
		return nil, false
	}
	// The rendered grid of the current sheet is the one being built, so a
	// reference into another sheet is resolved from the stored results.
	if vals, ok := r.cellValues[r.sheetIndex]; ok {
		if v, ok := vals[[2]int{row, col}]; ok {
			return v, true
		}
	}
	for i, sh := range r.renderedSheets {
		if !strings.EqualFold(sh.Name, sheet) {
			continue
		}
		for _, c := range sh.Cells {
			if c.R == row && c.C == col {
				return c.Text, true
			}
		}
		_ = i
	}
	return nil, false
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
		oc := OutCell{R: ct.R, C: ct.C, RS: ct.RS, CS: ct.CS, Style: ct.Style, Widget: ct.Widget}
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
	oc := OutCell{R: start, C: ct.C, RS: end - start, CS: ct.CS, Style: ct.Style, Text: v.r.display(ct, raw), Widget: ct.Widget}
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
		oc := OutCell{R: outRow, C: ct.C, RS: 1, CS: ct.CS, Style: ct.Style, Widget: ct.Widget}
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
		oc := OutCell{R: outRow, C: ct.C, RS: ct.RS, CS: ct.CS, Style: ct.Style, Widget: ct.Widget}
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
					oc := OutCell{R: row, C: outC, RS: ct.RS, CS: ct.CS, Style: ct.Style, Widget: ct.Widget}
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
			oc := OutCell{R: row, C: outC, RS: ct.RS, CS: outCS, Style: ct.Style, Widget: ct.Widget}
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

package fr

import (
	"fmt"
	"strconv"
	"strings"
)

// sliceSpec describes a FineReport cell slice, written as
// "C4[!0;!0]A{cond}".
//
// The text between the brackets names a region in FineReport's own notation:
//
//	!0              the whole expansion area of the current cell
//	B4:-1           the rows from B4 up to one row above it
//	A4:-1,B4:!-1    a two dimensional region
//
// The part before the ";" is the row direction and the part after it is the
// column direction. The optional text between braces filters the region by a
// condition such as "C4 > $C4".
type sliceSpec struct {
	// rowPart and colPart are the raw direction texts.
	rowPart, colPart string
	// hasRow and hasCol report whether a direction was given at all.
	hasRow, hasCol bool
}

// axis tells which dimension a direction applies to.
type axis int

const (
	axisRow axis = iota
	axisCol
)

// parseSliceSpec splits the text between the brackets of a slice.
//
// FineReport uses two separators and the meaning depends on the content:
//
//	[!0;!0]          a semicolon separates the row and column directions
//	[A4:-1,B4:+0]    a comma separates them as well
//	[E163,E164]      a comma separates a plain list of cells
//
// So a comma only separates directions when the parts look like a region,
// which means they carry an offset such as "-1" or "!:0".
func parseSliceSpec(spec string) (sliceSpec, error) {
	out := sliceSpec{}
	s := strings.TrimSpace(spec)
	if s == "" {
		return out, fmt.Errorf("切片区域为空")
	}
	// A semicolon always separates the two directions.
	if i := strings.Index(s, ";"); i >= 0 {
		out.rowPart = strings.TrimSpace(s[:i])
		out.colPart = strings.TrimSpace(s[i+1:])
		out.hasRow = out.rowPart != ""
		out.hasCol = out.colPart != ""
		return out, nil
	}
	// With a comma the parts are directions only when they carry an offset.
	if i := strings.Index(s, ","); i >= 0 {
		left := strings.TrimSpace(s[:i])
		right := strings.TrimSpace(s[i+1:])
		if looksLikeDirection(left) && looksLikeDirection(right) {
			out.rowPart = left
			out.colPart = right
			out.hasRow = true
			out.hasCol = true
			return out, nil
		}
	}
	out.rowPart = s
	out.hasRow = s != ""
	return out, nil
}

// looksLikeDirection reports whether a part of a slice names a region rather
// than a plain cell. A region carries an offset after a colon.
func looksLikeDirection(part string) bool {
	if part == "" {
		return false
	}
	if strings.HasPrefix(part, "!") {
		return true
	}
	_, _, ok := strings.Cut(part, ":")
	return ok
}

// sliceDirection is one direction of a slice, resolved into a coordinate
// range.
type sliceDirection struct {
	// whole means the direction covers the whole expansion area.
	whole bool
	// from and to are the coordinates of the region.
	from, to int
}

// resolveDirection turns a direction text into a coordinate range.
//
// The notation is "<cell>:<offset>" or just "!0". Both are FineReport's
// hierarchical coordinates: the offset counts along the expansion axis, so
// "B2:-1" is the level one above the current one and "!0" is the level the
// cell itself is on.
//
// The base is the coordinate of the cell the slice is written on: the
// dataset row for a row direction and the template column for a column
// direction.
func resolveDirection(text string, ax axis, base int) (sliceDirection, error) {
	d := sliceDirection{}
	text = strings.TrimSpace(text)
	if text == "" {
		return d, nil
	}
	if strings.HasPrefix(text, "!") {
		// "!0" is the whole level; other offsets are relative to the base.
		off, err := strconv.Atoi(strings.TrimSpace(text[1:]))
		if err != nil {
			return d, fmt.Errorf("切片偏移 %q 无效", text)
		}
		if off == 0 {
			d.whole = true
			return d, nil
		}
		d.from, d.to = base+off, base
		return d, nil
	}	// "<cell>:<offset>"
	cellPart, offPart, ok := strings.Cut(text, ":")
	if !ok {
		return d, fmt.Errorf("切片方向 %q 缺少偏移量", text)
	}
	col, row, valid := CellRefParts(strings.ToUpper(strings.TrimSpace(cellPart)))
	if !valid {
		return d, fmt.Errorf("切片基准 %q 不是单元格", cellPart)
	}
	off, err := parseSliceOffset(offPart)
	if err != nil {
		return d, err
	}
	// The hierarchical offset runs along the expansion axis. Both
	// directions step through the expanded rows, because a vertical
	// expansion has one growing dimension; the base cell only names the
	// level the step starts from.
	_ = col
	_ = row
	_ = ax
	d.from, d.to = base+off, base
	return d, nil
}

// parseSliceOffset reads an offset that may carry the "!" prefix, so both
// "-1" and "!-1" are accepted.
func parseSliceOffset(text string) (int, error) {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "!")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("切片偏移 %q 无效", text)
	}
	return n, nil
}

// sliceValues evaluates a cell slice on a vertical expansion.
//
// The two directions of the slice pick a region of the expanded rows:
//
//	[!0]        every expanded row of the column
//	[B2:-1]     the level one above the current one
//	[!0;!0]     every expanded row of every expanded column
//
// The offsets are hierarchical: they count along the expansion axis, so the
// base is the dataset row the formula sits on.
//
// The optional condition filters the region, and it is evaluated with the
// cell references resolved inside the expansion, so "C4 > $C4" compares the
// current row's C4 against the fixed C4.
func (v *vertRenderer) sliceValues(cell, spec, condition string, curRow int) (any, error) {
	sp, err := parseSliceSpec(spec)
	if err != nil {
		return nil, err
	}
	baseCol, _, ok := CellRefParts(cell)
	if !ok {
		return nil, fmt.Errorf("切片基准 %q 不是单元格", cell)
	}
	rowDir, err := resolveDirection(sp.rowPart, axisRow, curRow)
	if err != nil {
		return nil, err
	}
	// The column direction steps along the same axis, so it shares the
	// current row as its base.
	colDir, err := resolveDirection(sp.colPart, axisCol, curRow)
	if err != nil {
		return nil, err
	}
	// The rows the slice covers.
	rows := []int{}
	switch {
	case rowDir.whole:
		rows = v.expandedRows()
	case sp.hasRow:
		rows = v.rowsForLevel(rowDir.from, rowDir.to)
	default:
		rows = []int{curRow}
	}
	// The columns the slice covers. A vertical expansion grows in one
	// dimension, so a column direction selects the base column unless it
	// names the whole level.
	cols := []int{}
	switch {
	case colDir.whole:
		cols = v.expandedCols()
	case sp.hasCol:
		cols = []int{baseCol}
	default:
		cols = []int{baseCol}
	}
	cond := strings.TrimSpace(condition)
	out := []any{}
	for _, d := range rows {
		for _, c := range cols {
			val, ok := v.sliceCellValue(d, c)
			if !ok {
				continue
			}
			if cond != "" {
				keep, err := v.sliceConditionHolds(cond, d, c)
				if err != nil {
					return nil, err
				}
				if !keep {
					continue
				}
			}
			out = append(out, val)
		}
	}
	// A slice that names a single level yields that level's value; a slice
	// over the whole expansion yields the vector of its values.
	if !rowDir.whole && !colDir.whole && len(out) > 0 {
		return out[0], nil
	}
	return out, nil
}

// expandedRows lists the dataset row indexes the expansion covers.
func (v *vertRenderer) expandedRows() []int {
	if v.band == nil || v.band.ds == nil {
		return nil
	}
	n := len(v.band.ds.Rows)
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, i)
	}
	return out
}

// expandedCols lists the column indexes the expansion covers.
func (v *vertRenderer) expandedCols() []int {
	if v.band == nil {
		return nil
	}
	out := make([]int, 0, len(v.band.cells))
	for _, ct := range v.band.cells {
		out = append(out, ct.C)
	}
	return out
}

// rowsForLevel maps a hierarchical offset onto the dataset rows it covers.
// The levels count along the expansion axis, so "one above" is the previous
// dataset row.
func (v *vertRenderer) rowsForLevel(from, to int) []int {
	if from > to {
		from, to = to, from
	}
	out := []int{}
	for d := from; d <= to; d++ {
		if d < 0 {
			continue
		}
		if v.band != nil && v.band.ds != nil && d >= len(v.band.ds.Rows) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// colsForRange maps a template column range onto the expansion's columns.
func (v *vertRenderer) colsForRange(from, to int) []int {
	if from > to {
		from, to = to, from
	}
	out := []int{}
	for _, c := range v.expandedCols() {
		if c >= from && c <= to {
			out = append(out, c)
		}
	}
	return out
}

// sliceCellValue reads one cell of the slice region.
//
// A cell that is still being computed (the formula refers back to itself
// through the slice) falls back to the dataset's raw value. That is what
// makes FineReport's "running total" pattern work: the first row starts from
// zero and every later row adds the level above it.
func (v *vertRenderer) sliceCellValue(d, c int) (any, bool) {
	if v.band == nil {
		return nil, false
	}
	ct := cellAtCol(v.band.cells, c)
	if ct == nil {
		return nil, false
	}
	if v.bandBusy[[2]int{d, c}] {
		// The cell is mid-computation, so its own value cannot be used;
		// the dataset value is the only one available.
		return v.r.dsValue(ct, d), true
	}
	return v.bandCellRaw(ct, d), true
}

// sliceConditionHolds evaluates the filter of a slice for one cell of the
// region. The cell references inside the condition resolve inside the
// expansion, so they follow the row being tested.
func (v *vertRenderer) sliceConditionHolds(cond string, d, c int) (bool, error) {
	ctx := NewEvalContext()
	ctx.Params = v.r.paramVal
	ctx.Ref = func(ref string) (any, bool) {
		col, row, ok := CellRefParts(ref)
		if !ok {
			return nil, false
		}
		if v.band != nil && row == v.band.row {
			tc := cellAtCol(v.band.cells, col)
			if tc == nil {
				return nil, false
			}
			return v.bandCellRaw(tc, d), true
		}
		return v.outRef(row, col)
	}
	ctx.Range = func(a, b string) (any, bool) {
		return v.bandRange(a, b, []int{d})
	}
	res, err := EvalFormula(cond, ctx)
	if err != nil {
		return false, err
	}
	return truthy(scalar(res)), nil
}

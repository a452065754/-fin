package fr

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EvalContext supplies parameters, cell references and expansion state to the
// formula evaluator. Ref/Range may return a scalar or a []any vector.
type EvalContext struct {
	Params map[string]any
	Ref    func(ref string) (any, bool)
	Range  func(start, end string) (any, bool)
	Ident  func(name string) (any, bool)
	Seq    int
	Now    time.Time
	// ReportCell resolves FineReport's cross-report reference
	// VALUE("report name", row, column). The row and column are 1-based.
	ReportCell func(report string, row, col int) (any, bool)
	// SheetCell resolves a cross-sheet reference such as '2010-实际'!N5.
	SheetCell func(sheet, ref string) (any, bool)
	// DatasetColumn exposes a dataset column so functions such as
	// TABLEDATAFIELDS can list the available fields.
	DatasetColumn func(dataset, column string) (any, bool)
	// DatasetFields lists the column names of a dataset.
	DatasetFields func(dataset string) ([]string, bool)
	// DatasetColumnValues returns every value of one dataset column. It is
	// what FineReport's dataset.select(column) method does.
	DatasetColumnValues func(dataset, column string) ([]any, bool)
	// DatasetGroup returns the value of a dataset group, which is what
	// FineReport's dataset.group(key) method does.
	DatasetGroup func(dataset string, key any) (any, bool)
}

func NewEvalContext() *EvalContext {
	return &EvalContext{Params: map[string]any{}, Now: time.Now()}
}

type formulaExpr interface {
	eval(*EvalContext) (any, error)
}

type litExpr struct{ v any }

func (e litExpr) eval(*EvalContext) (any, error) { return e.v, nil }

type refExpr struct{ ref string }

func (e refExpr) eval(ctx *EvalContext) (any, error) {
	if ctx.Ref == nil {
		return nil, fmt.Errorf("无法解析单元格 %s", e.ref)
	}
	v, ok := ctx.Ref(e.ref)
	if !ok {
		return nil, fmt.Errorf("无效单元格 %s", e.ref)
	}
	return v, nil
}

type rangeExpr struct{ start, end string }

func (e rangeExpr) eval(ctx *EvalContext) (any, error) {
	if ctx.Range == nil {
		return nil, fmt.Errorf("不支持区域 %s:%s", e.start, e.end)
	}
	v, ok := ctx.Range(e.start, e.end)
	if !ok {
		return nil, fmt.Errorf("无效区域 %s:%s", e.start, e.end)
	}
	return v, nil
}

type paramExpr struct{ name string }

func (e paramExpr) eval(ctx *EvalContext) (any, error) {
	for k, v := range ctx.Params {
		if strings.EqualFold(k, e.name) {
			return v, nil
		}
	}
	return "", nil
}

// arrayExpr evaluates FineReport's "[a, b, c]A" literal into a vector.
type arrayExpr struct{ items []formulaExpr }

func (e arrayExpr) eval(ctx *EvalContext) (any, error) {
	out := make([]any, 0, len(e.items))
	for _, it := range e.items {
		v, err := it.eval(ctx)
		if err != nil {
			return nil, err
		}
		if vec, ok := v.([]any); ok {
			out = append(out, vec...)
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// setExpr evaluates FineReport's "{a, b}" literal into a deduplicated vector.
type setExpr struct{ items []formulaExpr }

func (e setExpr) eval(ctx *EvalContext) (any, error) {
	v, err := arrayExpr{items: e.items}.eval(ctx)
	if err != nil {
		return nil, err
	}
	vec, _ := v.([]any)
	seen := map[string]bool{}
	out := make([]any, 0, len(vec))
	for _, item := range vec {
		key := stringOf(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, nil
}

// sheetRefExpr evaluates a cross-sheet reference such as '2010-实际'!N5.
type sheetRefExpr struct {
	sheet string
	ref   string
}

func (e sheetRefExpr) eval(ctx *EvalContext) (any, error) {
	if ctx.SheetCell == nil {
		return nil, fmt.Errorf("无法解析工作表 %s 的单元格 %s", e.sheet, e.ref)
	}
	v, ok := ctx.SheetCell(e.sheet, e.ref)
	if !ok {
		return nil, fmt.Errorf("工作表 %s 中没有单元格 %s", e.sheet, e.ref)
	}
	return v, nil
}

// methodCallExpr evaluates a dataset method call such as
// ds1.select(销售额) or ds1.group(2011).
type methodCallExpr struct {
	receiver string
	method   string
	args     []formulaExpr
}

// argText returns the raw text of an argument. It is used where FineReport
// expects a name rather than a value, such as a dataset column or a filter
// condition. A comparison is rendered back to its source form.
func (e methodCallExpr) argText(i int) string {
	if i >= len(e.args) {
		return ""
	}
	switch a := e.args[i].(type) {
	case litExpr:
		return stringOf(a.v)
	case identExpr:
		return a.name
	case paramExpr:
		return a.name
	case binExpr:
		return exprText(a.l) + a.op + exprText(a.r)
	}
	return ""
}

// exprText renders a literal or identifier back to source text.
func exprText(e formulaExpr) string {
	switch a := e.(type) {
	case litExpr:
		return stringOf(a.v)
	case identExpr:
		return a.name
	case paramExpr:
		return a.name
	}
	return ""
}

func (e methodCallExpr) eval(ctx *EvalContext) (any, error) {
	name := strings.ToLower(e.method)
	switch name {
	case "select":
		if len(e.args) < 1 {
			return nil, fmt.Errorf("%s.select 缺少列名", e.receiver)
		}
		// The column name is an identifier, not an expression, so it is
		// taken verbatim instead of being evaluated.
		col := e.argText(0)
		if col == "" {
			return nil, fmt.Errorf("%s.select 缺少列名", e.receiver)
		}
		if ctx.DatasetColumnValues == nil {
			return nil, fmt.Errorf("无法读取数据集 %s", e.receiver)
		}
		values, ok := ctx.DatasetColumnValues(e.receiver, col)
		if !ok {
			return nil, fmt.Errorf("数据集 %s 中没有列 %s", e.receiver, col)
		}
		// A second argument filters the column by a condition such as
		// "公司名称=B37". The condition is raw text, so its column name
		// is taken verbatim instead of being evaluated.
		if len(e.args) > 1 {
			cond := e.argText(1)
			if cond == "" {
				return nil, fmt.Errorf("%s.select 的筛选条件无效", e.receiver)
			}
			filtered, err := filterByCondition(ctx, e.receiver, values, cond)
			if err != nil {
				return nil, err
			}
			values = filtered
		}
		return values, nil
	case "group":
		if len(e.args) < 1 {
			return nil, fmt.Errorf("%s.group 缺少参数", e.receiver)
		}
		if ctx.DatasetGroup == nil {
			return nil, fmt.Errorf("无法读取数据集 %s 的分组", e.receiver)
		}
		// The group key is a literal such as 2011.
		key := e.argText(0)
		if key == "" {
			return nil, fmt.Errorf("%s.group 缺少参数", e.receiver)
		}
		v, ok := ctx.DatasetGroup(e.receiver, key)
		if !ok {
			return nil, fmt.Errorf("数据集 %s 中没有分组 %s", e.receiver, key)
		}
		return v, nil
	}
	return nil, fmt.Errorf("暂不支持的数据集方法 %s.%s", e.receiver, e.method)
}

// filterByCondition keeps the values whose row matches a condition written
// as "column=reference", e.g. "公司名称=B37".
func filterByCondition(ctx *EvalContext, dataset string, values []any, cond string) ([]any, error) {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return values, nil
	}
	eq := strings.Index(cond, "=")
	if eq < 0 {
		return nil, fmt.Errorf("无法解析筛选条件 %q", cond)
	}
	colName := strings.TrimSpace(cond[:eq])
	want := strings.TrimSpace(cond[eq+1:])
	// The right hand side may be a cell reference.
	wantValue := want
	if ctx.Ref != nil {
		if v, ok := ctx.Ref(strings.ToUpper(want)); ok {
			wantValue = stringOf(v)
		}
	}
	if ctx.DatasetColumnValues == nil {
		return nil, fmt.Errorf("无法读取数据集列 %s", colName)
	}
	keys, ok := ctx.DatasetColumnValues(dataset, colName)
	if !ok {
		return nil, fmt.Errorf("数据集 %s 中没有列 %s", dataset, colName)
	}
	out := []any{}
	for i, k := range keys {
		if stringOf(k) != wantValue {
			continue
		}
		if i < len(values) {
			out = append(out, values[i])
		}
	}
	return out, nil
}

type identExpr struct{ name string }
func (e identExpr) eval(ctx *EvalContext) (any, error) {
	if ctx.Ident != nil {
		if v, ok := ctx.Ident(e.name); ok {
			return v, nil
		}
	}
	for k, v := range ctx.Params {
		if strings.EqualFold(k, e.name) {
			return v, nil
		}
	}
	return nil, fmt.Errorf("未知标识符 %s", e.name)
}

type unaryExpr struct {
	op string
	x  formulaExpr
}

func (e unaryExpr) eval(ctx *EvalContext) (any, error) {
	v, err := e.x.eval(ctx)
	if err != nil {
		return nil, err
	}
	if vec, ok := v.([]any); ok {
		out := make([]any, len(vec))
		for i := range vec {
			out[i], err = applyUnary(e.op, vec[i])
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return applyUnary(e.op, v)
}

func applyUnary(op string, v any) (any, error) {
	switch op {
	case "-":
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("无法对 %v 取负", v)
		}
		return -f, nil
	case "+":
		return v, nil
	case "&":
		return stringOf(v), nil
	case "not":
		return !truthy(v), nil
	}
	return nil, fmt.Errorf("未知一元运算符 %s", op)
}

type binExpr struct {
	op   string
	l, r formulaExpr
}

func (e binExpr) eval(ctx *EvalContext) (any, error) {
	lv, err := e.l.eval(ctx)
	if err != nil {
		return nil, err
	}
	rv, err := e.r.eval(ctx)
	if err != nil {
		return nil, err
	}
	lvec, lIsVec := lv.([]any)
	rvec, rIsVec := rv.([]any)
	if !lIsVec && !rIsVec {
		return applyBinary(e.op, lv, rv)
	}
	n := 0
	if lIsVec {
		n = len(lvec)
	}
	if rIsVec && len(rvec) > n {
		n = len(rvec)
	}
	out := make([]any, n)
	for i := 0; i < n; i++ {
		var a, b any
		if lIsVec {
			a = elemAt(lvec, i)
		} else {
			a = lv
		}
		if rIsVec {
			b = elemAt(rvec, i)
		} else {
			b = rv
		}
		out[i], err = applyBinary(e.op, a, b)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func elemAt(v []any, i int) any {
	if i < len(v) {
		return v[i]
	}
	return nil
}

func applyBinary(op string, a, b any) (any, error) {
	switch op {
	case "&":
		return stringOf(a) + stringOf(b), nil
	case "+":
		if _, aStr := a.(string); aStr {
			if _, ok := toFloat(a); !ok {
				return stringOf(a) + stringOf(b), nil
			}
		}
		if _, bStr := b.(string); bStr {
			if _, ok := toFloat(b); !ok {
				return stringOf(a) + stringOf(b), nil
			}
		}
		fa, oka := toFloat(a)
		fb, okb := toFloat(b)
		if !oka || !okb {
			return nil, fmt.Errorf("%v + %v 不是数值", a, b)
		}
		return fa + fb, nil
	case "-", "*", "/", "%", "^":
		fa, oka := toFloat(a)
		fb, okb := toFloat(b)
		if !oka || !okb {
			return nil, fmt.Errorf("运算符 %s 需要数值", op)
		}
		switch op {
		case "-":
			return fa - fb, nil
		case "*":
			return fa * fb, nil
		case "/":
			if fb == 0 {
				return nil, fmt.Errorf("除数不能为 0")
			}
			return fa / fb, nil
		case "%":
			if fb == 0 {
				return nil, fmt.Errorf("除数不能为 0")
			}
			return math.Mod(fa, fb), nil
		case "^":
			return math.Pow(fa, fb), nil
		}
	case "=", "==", "<>", "!=", "<", ">", "<=", ">=":
		cmp := 0
		fa, oka := toFloat(a)
		fb, okb := toFloat(b)
		if oka && okb {
			switch {
			case fa < fb:
				cmp = -1
			case fa > fb:
				cmp = 1
			}
		} else {
			sa, sb := stringOf(a), stringOf(b)
			cmp = strings.Compare(sa, sb)
		}
		switch op {
		case "=", "==":
			return cmp == 0, nil
		case "<>", "!=":
			return cmp != 0, nil
		case "<":
			return cmp < 0, nil
		case ">":
			return cmp > 0, nil
		case "<=":
			return cmp <= 0, nil
		case ">=":
			return cmp >= 0, nil
		}
	}
	return nil, fmt.Errorf("未知运算符 %s", op)
}

type callExpr struct {
	name string
	args []formulaExpr
}

func (e callExpr) eval(ctx *EvalContext) (any, error) {
	name := strings.ToUpper(e.name)
	// IF and IFERROR need lazy argument evaluation: FineReport templates
	// rely on the untaken branch never being computed, e.g.
	// "=if(sum(C9) = 0, null, sum(C9))" or "=IFERROR(1/0, 0)".
	switch name {
	case "IF":
		return e.evalIf(ctx)
	case "IFERROR":
		return e.evalIfError(ctx)
	case "GREPARRAY":
		return e.evalGrepArray(ctx)
	case "MAPARRAY":
		return e.evalMapArray(ctx)
	}
	vals := make([]any, len(e.args))
	for i, a := range e.args {
		v, err := a.eval(ctx)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	switch name {
	case "SUM":
		return sumValues(vals), nil
	case "AVERAGE", "AVG":
		s, n := sumAndCount(vals)
		if n == 0 {
			return nil, fmt.Errorf("AVERAGE 没有数值")
		}
		return s / float64(n), nil
	case "COUNT":
		_, n := sumAndCount(vals)
		return float64(n), nil
	case "COUNTA":
		n := 0
		for _, v := range flatten(vals) {
			if v != nil && stringOf(v) != "" {
				n++
			}
		}
		return float64(n), nil
	case "MAX", "MIN":
		fs := numbers(vals)
		if len(fs) == 0 {
			return 0.0, nil
		}
		best := fs[0]
		for _, f := range fs[1:] {
			if (name == "MAX" && f > best) || (name == "MIN" && f < best) {
				best = f
			}
		}
		return best, nil
	case "ROUND", "ROUNDUP", "ROUNDDOWN":
		if len(vals) < 1 {
			return nil, fmt.Errorf("%s 缺少参数", name)
		}
		digits := 0
		if len(vals) > 1 {
			if f, ok := toFloat(vals[1]); ok {
				digits = int(f)
			}
		}
		p := math.Pow(10, float64(digits))
		return vectorMap(vals[0], func(v any) (any, error) {
			f, ok := toFloat(v)
			if !ok {
				return nil, fmt.Errorf("%s 参数不是数值", name)
			}
			switch name {
			case "ROUND":
				return math.Round(f*p) / p, nil
			case "ROUNDUP":
				return math.Ceil(f*p) / p, nil
			default:
				return math.Floor(f*p) / p, nil
			}
		})
	case "INT", "FLOOR", "CEILING", "ABS", "SIGN", "SQRT":
		if len(vals) < 1 {
			return nil, fmt.Errorf("%s 缺少参数", name)
		}
		return vectorMap(vals[0], func(v any) (any, error) {
			f, ok := toFloat(v)
			if !ok {
				return nil, fmt.Errorf("%s 参数不是数值", name)
			}
			switch name {
			case "INT":
				return math.Floor(f), nil
			case "FLOOR":
				return math.Floor(f), nil
			case "CEILING":
				return math.Ceil(f), nil
			case "ABS":
				return math.Abs(f), nil
			case "SIGN":
				if f > 0 {
					return 1.0, nil
				} else if f < 0 {
					return -1.0, nil
				}
				return 0.0, nil
			default:
				return math.Sqrt(f), nil
			}
		})
	case "MOD":
		if len(vals) < 2 {
			return nil, fmt.Errorf("MOD 需要两个参数")
		}
		return applyBinary("%", vals[0], vals[1])
	case "POWER":
		if len(vals) < 2 {
			return nil, fmt.Errorf("POWER 需要两个参数")
		}
		return applyBinary("^", vals[0], vals[1])
	case "IF":
		// Handled lazily in evalIf.
		return nil, fmt.Errorf("IF 需要至少两个参数")
	case "AND", "OR":
		acc := name == "AND"
		found := false
		for _, v := range flatten(vals) {
			if v == nil || stringOf(v) == "" {
				continue
			}
			found = true
			if name == "AND" {
				acc = acc && truthy(v)
			} else {
				acc = acc || truthy(v)
			}
		}
		if !found {
			return name == "AND", nil
		}
		return acc, nil
	case "NOT":
		if len(vals) < 1 {
			return nil, fmt.Errorf("NOT 缺少参数")
		}
		return !truthy(vals[0]), nil
	case "FORMAT", "TEXT":
		if len(vals) < 1 {
			return nil, fmt.Errorf("%s 缺少参数", name)
		}
		pat := ""
		if len(vals) > 1 {
			pat = stringOf(vals[1])
		}
		return FormatValue(vals[0], pat), nil
	case "CONCATENATE":
		return vectorMapMany(vals, func(items []any) (any, error) {
			var b strings.Builder
			for _, it := range items {
				b.WriteString(stringOf(it))
			}
			return b.String(), nil
		})
	case "LEFT", "RIGHT":
		if len(vals) < 1 {
			return nil, fmt.Errorf("%s 缺少参数", name)
		}
		n := 1.0
		if len(vals) > 1 {
			n, _ = toFloat(vals[1])
		}
		return vectorMap(vals[0], func(v any) (any, error) {
			rs := []rune(stringOf(v))
			k := int(n)
			if k < 0 {
				k = 0
			}
			if k > len(rs) {
				k = len(rs)
			}
			if name == "LEFT" {
				return string(rs[:k]), nil
			}
			return string(rs[len(rs)-k:]), nil
		})
	case "MID":
		if len(vals) < 3 {
			return nil, fmt.Errorf("MID 需要三个参数")
		}
		startF, _ := toFloat(vals[1])
		lenF, _ := toFloat(vals[2])
		return vectorMap(vals[0], func(v any) (any, error) {
			rs := []rune(stringOf(v))
			start := int(startF) - 1
			if start < 0 {
				start = 0
			}
			if start > len(rs) {
				return "", nil
			}
			end := start + int(lenF)
			if end > len(rs) {
				end = len(rs)
			}
			return string(rs[start:end]), nil
		})
	case "LEN":
		if len(vals) < 1 {
			return nil, fmt.Errorf("LEN 缺少参数")
		}
		return float64(len([]rune(stringOf(vals[0])))), nil
	case "TRIM":
		if len(vals) < 1 {
			return "", nil
		}
		return strings.TrimSpace(stringOf(vals[0])), nil
	case "UPPER", "LOWER":
		if len(vals) < 1 {
			return "", nil
		}
		if strings.EqualFold(name, "UPPER") {
			return strings.ToUpper(stringOf(vals[0])), nil
		}
		return strings.ToLower(stringOf(vals[0])), nil
	case "VALUE":
		// VALUE(text) converts text to a number. VALUE(report, row, col)
		// reads a cell from another report, which is how FineReport links
		// a narrative report to its data tables.
		if len(vals) >= 3 {
			name := stringOf(vals[0])
			row, ok1 := toFloat(vals[1])
			col, ok2 := toFloat(vals[2])
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("VALUE 的行列参数不是数值")
			}
			if ctx.ReportCell == nil {
				return nil, fmt.Errorf("无法解析报表 %s 的单元格", name)
			}
			v, ok := ctx.ReportCell(name, int(row), int(col))
			if !ok {
				return nil, fmt.Errorf("报表 %s 中找不到单元格 (%d,%d)", name, int(row), int(col))
			}
			return v, nil
		}
		if len(vals) < 1 {
			return 0.0, nil
		}
		f, _ := toFloat(vals[0])
		return f, nil
	case "RANGE":
		// RANGE(start, end) builds an inclusive sequence of numbers.
		if len(vals) < 2 {
			return nil, fmt.Errorf("RANGE 需要两个参数")
		}
		a, ok1 := toFloat(vals[0])
		b, ok2 := toFloat(vals[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("RANGE 参数不是数值")
		}
		if a > b {
			a, b = b, a
		}
		out := []any{}
		for v := a; v <= b; v++ {
			out = append(out, v)
			if len(out) > 100000 {
				break
			}
		}
		return out, nil
	case "ARRAY":
		// ARRAY(a, b, c) builds a vector from its arguments.
		out := make([]any, 0, len(vals))
		for _, v := range vals {
			if vec, ok := v.([]any); ok {
				out = append(out, vec...)
				continue
			}
			out = append(out, v)
		}
		return out, nil
	case "REVERSEARRAY":
		if len(vals) < 1 {
			return []any{}, nil
		}
		items := flatten(vals[0:1])
		out := make([]any, len(items))
		for i, v := range items {
			out[len(items)-1-i] = v
		}
		return out, nil
	case "TABLEDATAFIELDS":
		// TABLEDATAFIELDS("ds1") lists the columns of a dataset.
		if len(vals) < 1 {
			return []any{}, nil
		}
		if ctx.DatasetFields == nil {
			return nil, fmt.Errorf("无法读取数据集字段")
		}
		cols, ok := ctx.DatasetFields(stringOf(vals[0]))
		if !ok {
			return nil, fmt.Errorf("找不到数据集 %s", stringOf(vals[0]))
		}
		out := make([]any, len(cols))
		for i, c := range cols {
			out[i] = c
		}
		return out, nil
	case "TODAY":
		t := ctx.now()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()), nil
	case "NOW":
		return ctx.now().Format("2006-01-02 15:04:05"), nil
	case "SEQ":
		if ctx.Seq > 0 {
			return float64(ctx.Seq), nil
		}
		return 1.0, nil
	case "YEAR", "MONTH", "DAY":
		t := ctx.now()
		if len(vals) >= 1 {
			tt, ok := toTime(vals[0])
			if !ok {
				return 0.0, nil
			}
			t = tt
		}
		switch name {
		case "YEAR":
			return float64(t.Year()), nil
		case "MONTH":
			return float64(t.Month()), nil
		default:
			return float64(t.Day()), nil
		}
	case "RAND":
		return rand.Float64(), nil
	case "WEEKDAY":
		t := ctx.now()
		if len(vals) >= 1 {
			tt, ok := toTime(vals[0])
			if !ok {
				return 0.0, nil
			}
			t = tt
		}
		return float64(t.Weekday()), nil
	case "SWITCH":
		if len(vals) < 3 {
			return nil, fmt.Errorf("SWITCH 参数不足")
		}
		for i := 1; i+1 < len(vals); i += 2 {
			if compareRaw(vals[0], vals[i]) == 0 {
				return scalar(vals[i+1]), nil
			}
		}
		if len(vals)%2 == 0 {
			return scalar(vals[len(vals)-1]), nil
		}
		return nil, nil
	case "ISNULL", "EMPTY":
		if len(vals) < 1 {
			return true, nil
		}
		return vals[0] == nil || stringOf(vals[0]) == "", nil
	case "RANDBETWEEN":
		if len(vals) < 2 {
			return nil, fmt.Errorf("RANDBETWEEN 需要两个参数")
		}
		lo, ok1 := toFloat(vals[0])
		hi, ok2 := toFloat(vals[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("RANDBETWEEN 参数不是数值")
		}
		if hi < lo {
			lo, hi = hi, lo
		}
		// Keep the result inside [lo, hi] even when the range is fractional.
		span := hi - lo
		if span <= 0 {
			return lo, nil
		}
		return lo + rand.Float64()*span, nil
	case "REPEAT":
		if len(vals) < 1 {
			return "", nil
		}
		n, _ := toFloat(vals[len(vals)-1])
		cnt := int(n)
		if cnt < 0 {
			cnt = 0
		}
		return strings.Repeat(stringOf(vals[0]), cnt), nil
	case "SPLIT":
		if len(vals) < 1 {
			return []any{}, nil
		}
		sep := ""
		if len(vals) > 1 {
			sep = stringOf(vals[1])
		}
		s := stringOf(vals[0])
		if sep == "" {
			return []any{s}, nil
		}
		parts := strings.Split(s, sep)
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out, nil
	case "REPLACE":
		// FineReport's REPLACE(text, old, new) replaces every occurrence.
		if len(vals) < 3 {
			return nil, fmt.Errorf("REPLACE 需要三个参数")
		}
		return strings.ReplaceAll(stringOf(vals[0]), stringOf(vals[1]), stringOf(vals[2])), nil
	case "FIND":
		// FineReport's FIND(needle, haystack) puts the needle first and
		// returns the 1-based position, or 0 when not found.
		if len(vals) < 2 {
			return nil, fmt.Errorf("FIND 需要两个参数")
		}
		idx := strings.Index(stringOf(vals[1]), stringOf(vals[0]))
		if idx < 0 {
			return 0.0, nil
		}
		return float64(idx + 1), nil
	case "INDEXOFARRAY":
		// FineReport's INDEXOFARRAY(value, array) puts the value first and
		// returns the 1-based position, or 0 when not found.
		if len(vals) < 2 {
			return nil, fmt.Errorf("INDEXOFARRAY 需要两个参数")
		}
		for i, v := range flatten(vals[1:2]) {
			if compareRaw(v, vals[0]) == 0 {
				return float64(i + 1), nil
			}
		}
		return 0.0, nil
	case "INARRAY":
		// FineReport's INARRAY(value, array) puts the value first.
		if len(vals) < 2 {
			return nil, fmt.Errorf("INARRAY 需要两个参数")
		}
		for i, v := range flatten(vals[1:2]) {
			if compareRaw(v, vals[0]) == 0 {
				return float64(i + 1), nil
			}
		}
		return 0.0, nil
	case "JOINARRAY":
		if len(vals) < 1 {
			return "", nil
		}
		sep := ""
		if len(vals) > 1 {
			sep = stringOf(vals[1])
		}
		items := flatten(vals[0:1])
		parts := make([]string, len(items))
		for i, v := range items {
			parts[i] = stringOf(v)
		}
		return strings.Join(parts, sep), nil
	case "UNIQUEARRAY":
		if len(vals) < 1 {
			return []any{}, nil
		}
		items := flatten(vals[0:1])
		seen := map[string]bool{}
		out := []any{}
		for _, v := range items {
			key := stringOf(v)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, v)
		}
		return out, nil
	case "SORT":
		if len(vals) < 1 {
			return []any{}, nil
		}
		desc := false
		if len(vals) > 1 {
			desc = truthy(vals[1])
		}
		items := append([]any{}, flatten(vals[0:1])...)
		sort.SliceStable(items, func(i, j int) bool {
			c := compareRaw(items[i], items[j])
			if desc {
				return c > 0
			}
			return c < 0
		})
		return items, nil
	case "ACCSUM":
		// ACCSUM(values) accumulates, ACCSUM(values, reset) restarts at the
		// given seed value.
		if len(vals) < 1 {
			return 0.0, nil
		}
		items := flatten(vals[0:1])
		acc := 0.0
		if len(vals) > 1 {
			acc, _ = toFloat(vals[1])
		}
		for _, v := range items {
			if f, ok := toFloat(v); ok {
				acc += f
			}
		}
		return acc, nil
	case "MOM":
		// MOM(current, previous) is the month-over-month growth rate.
		if len(vals) < 2 {
			return nil, fmt.Errorf("MOM 需要两个参数")
		}
		cur, ok1 := toFloat(vals[0])
		prev, ok2 := toFloat(vals[1])
		if !ok1 || !ok2 || prev == 0 {
			return nil, nil
		}
		return (cur - prev) / prev, nil
	case "UUID":
		n := 32
		if len(vals) > 0 {
			if f, ok := toFloat(vals[0]); ok {
				n = int(f)
			}
		}
		if n <= 0 {
			n = 32
		}
		return randomHex(n), nil
	case "DATE":
		// DATE(year, month, day)
		if len(vals) < 3 {
			return nil, fmt.Errorf("DATE 需要三个参数")
		}
		y, _ := toFloat(vals[0])
		m, _ := toFloat(vals[1])
		d, _ := toFloat(vals[2])
		return time.Date(int(y), time.Month(int(m)), int(d), 0, 0, 0, 0, time.Local), nil
	case "TODATE":
		if len(vals) < 1 {
			return nil, fmt.Errorf("TODATE 缺少参数")
		}
		if t, ok := toTime(vals[0]); ok {
			return t, nil
		}
		return nil, fmt.Errorf("TODATE 无法解析日期 %v", vals[0])
	case "DATETONUMBER":
		// DATETONUMBER returns the day count since the epoch.
		if len(vals) < 1 {
			return nil, fmt.Errorf("DATETONUMBER 缺少参数")
		}
		t, ok := toTime(vals[0])
		if !ok {
			return nil, fmt.Errorf("DATETONUMBER 无法解析日期 %v", vals[0])
		}
		return float64(t.Unix() / 86400), nil
	case "DATEDELTA":
		// DATEDELTA(date, days) shifts a date by whole days.
		if len(vals) < 2 {
			return nil, fmt.Errorf("DATEDELTA 需要两个参数")
		}
		t, ok := toTime(vals[0])
		if !ok {
			return nil, fmt.Errorf("DATEDELTA 无法解析日期 %v", vals[0])
		}
		days, _ := toFloat(vals[1])
		return t.AddDate(0, 0, int(days)), nil
	case "MONTHDELTA":
		// MONTHDELTA(date, months) shifts a date by whole months.
		if len(vals) < 2 {
			return nil, fmt.Errorf("MONTHDELTA 需要两个参数")
		}
		t, ok := toTime(vals[0])
		if !ok {
			return nil, fmt.Errorf("MONTHDELTA 无法解析日期 %v", vals[0])
		}
		months, _ := toFloat(vals[1])
		return t.AddDate(0, int(months), 0), nil
	case "DATEINMONTH":
		// DATEINMONTH(date, offset) returns the first day of the month that
		// is offset months away from the given date.
		if len(vals) < 2 {
			return nil, fmt.Errorf("DATEINMONTH 需要两个参数")
		}
		t, ok := toTime(vals[0])
		if !ok {
			return nil, fmt.Errorf("DATEINMONTH 无法解析日期 %v", vals[0])
		}
		months, _ := toFloat(vals[1])
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, int(months), 0), nil
	case "DATEINWEEK":
		// DATEINWEEK(date, offset) returns the first day of the week that is
		// offset weeks away. Weeks start on Sunday.
		if len(vals) < 2 {
			return nil, fmt.Errorf("DATEINWEEK 需要两个参数")
		}
		t, ok := toTime(vals[0])
		if !ok {
			return nil, fmt.Errorf("DATEINWEEK 无法解析日期 %v", vals[0])
		}
		weeks, _ := toFloat(vals[1])
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		start := day.AddDate(0, 0, -int(day.Weekday()))
		return start.AddDate(0, 0, int(weeks)*7), nil
	case "DATEDIF":
		// DATEDIF(start, end, unit) where unit is "D", "M" or "Y".
		if len(vals) < 3 {
			return nil, fmt.Errorf("DATEDIF 需要三个参数")
		}
		a, ok1 := toTime(vals[0])
		b, ok2 := toTime(vals[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("DATEDIF 无法解析日期")
		}
		unit := strings.ToUpper(strings.TrimSpace(stringOf(vals[2])))
		switch unit {
		case "D":
			return float64(int(b.Sub(a).Hours() / 24)), nil
		case "M":
			months := (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
			if b.Day() < a.Day() {
				months--
			}
			return float64(months), nil
		case "Y":
			years := b.Year() - a.Year()
			if b.Month() < a.Month() || (b.Month() == a.Month() && b.Day() < a.Day()) {
				years--
			}
			return float64(years), nil
		}
		return nil, fmt.Errorf("DATEDIF 不支持的单位 %s", unit)
	case "CNMONEY":
		// CNMONEY converts a number into Chinese capitalised currency text.
		if len(vals) < 1 {
			return "", nil
		}
		f, ok := toFloat(vals[0])
		if !ok {
			return "", nil
		}
		return chineseMoney(f), nil
	case "IFERROR":
		// Handled lazily in evalIfError so that a failing first argument is
		// caught instead of aborting the whole expression.
		return nil, fmt.Errorf("IFERROR 需要至少一个参数")
	}
	return nil, fmt.Errorf("暂不支持的函数 %s", e.name)
}

// evalGrepArray evaluates GREPARRAY(array, condition). The condition is
// evaluated once per element with "item" bound to that element, so only the
// matching elements are kept.
func (e callExpr) evalGrepArray(ctx *EvalContext) (any, error) {
	if len(e.args) < 2 {
		return nil, fmt.Errorf("GREPARRAY 需要两个参数")
	}
	arr, err := e.args[0].eval(ctx)
	if err != nil {
		return nil, err
	}
	items, _ := arr.([]any)
	out := []any{}
	for _, item := range items {
		child := ctx.withItem(item)
		ok, err := e.args[1].eval(child)
		if err != nil {
			return nil, err
		}
		if truthy(scalar(ok)) {
			out = append(out, item)
		}
	}
	return out, nil
}

// evalMapArray evaluates MAPARRAY(array, expression). The expression is
// evaluated once per element with "item" bound to that element.
func (e callExpr) evalMapArray(ctx *EvalContext) (any, error) {
	if len(e.args) < 2 {
		return nil, fmt.Errorf("MAPARRAY 需要两个参数")
	}
	arr, err := e.args[0].eval(ctx)
	if err != nil {
		return nil, err
	}
	items, _ := arr.([]any)
	out := make([]any, 0, len(items))
	for _, item := range items {
		child := ctx.withItem(item)
		v, err := e.args[1].eval(child)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// withItem returns a shallow copy of the context with "item" bound, which is
// how the array functions expose the current element to their expression.
func (ctx *EvalContext) withItem(item any) *EvalContext {
	child := *ctx
	child.Params = make(map[string]any, len(ctx.Params)+1)
	for k, v := range ctx.Params {
		child.Params[k] = v
	}
	child.Params["item"] = item
	return &child
}

// evalIf evaluates IF lazily so the branch that is not taken is never
// computed. A vector condition maps element by element.
func (e callExpr) evalIf(ctx *EvalContext) (any, error) {
	if len(e.args) < 2 {
		return nil, fmt.Errorf("IF 需要至少两个参数")
	}
	cond, err := e.args[0].eval(ctx)
	if err != nil {
		return nil, err
	}
	pick := func(c any) (any, error) {
		if truthy(c) {
			v, err := e.args[1].eval(ctx)
			if err != nil {
				return nil, err
			}
			return scalar(v), nil
		}
		if len(e.args) > 2 {
			v, err := e.args[2].eval(ctx)
			if err != nil {
				return nil, err
			}
			return scalar(v), nil
		}
		return false, nil
	}
	if vec, ok := cond.([]any); ok {
		out := make([]any, len(vec))
		for i := range vec {
			v, err := pick(vec[i])
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	return pick(cond)
}

// evalIfError evaluates IFERROR lazily: the first argument is attempted and
// its error is swallowed in favour of the second argument.
func (e callExpr) evalIfError(ctx *EvalContext) (any, error) {
	if len(e.args) < 1 {
		return nil, fmt.Errorf("IFERROR 需要至少一个参数")
	}
	v, err := e.args[0].eval(ctx)
	if err == nil {
		return v, nil
	}
	if len(e.args) > 1 {
		alt, aerr := e.args[1].eval(ctx)
		if aerr != nil {
			return nil, aerr
		}
		return alt, nil
	}
	return nil, nil
}

func (ctx *EvalContext) now() time.Time {
	if ctx.Now.IsZero() {
		return time.Now()
	}
	return ctx.Now
}

// randomHex returns a random lowercase hex string of n characters.
func randomHex(n int) string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexDigits[rand.Intn(len(hexDigits))]
	}
	return string(b)
}

// chineseDigits maps a digit to its Chinese capitalised form. The Chinese
// numerals are not contiguous in Unicode, so an explicit table is required.
var chineseDigits = []string{"零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"}

// chineseMoney renders a number as Chinese capitalised currency text, the
// same way FineReport's CNMONEY does.
func chineseMoney(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	// Split into the integer part and two decimal digits.
	units := []string{"", "拾", "佰", "仟"}
	sections := []string{"", "万", "亿", "万亿"}
	intPart := int64(f)
	frac := int64(math.Round((f - float64(intPart)) * 100))
	if intPart == 0 && frac == 0 {
		return "零元整"
	}
	var secTexts []string
	idx := 0
	for intPart > 0 {
		group := intPart % 10000
		intPart /= 10000
		text := sectionText(group, units)
		if text != "" {
			text += sections[idx]
		}
		secTexts = append([]string{text}, secTexts...)
		idx++
		if idx >= len(sections) {
			break
		}
	}
	body := strings.TrimPrefix(strings.Join(secTexts, ""), "零")
	body = collapseZeros(body)
	var out string
	if body == "" {
		out = ""
	} else {
		out = body + "元"
	}
	switch {
	case frac == 0:
		out += "整"
	case frac < 10:
		out += "零" + chineseDigits[frac/10] + "分"
	default:
		out += chineseDigits[frac/10] + "角" + chineseDigits[frac%10] + "分"
	}
	if neg {
		out = "负" + out
	}
	return out
}

// sectionText renders a group of at most four digits with its unit suffixes.
// A zero digit in the middle keeps a "零" placeholder. Leading zeros are
// dropped, so 105 reads as 壹佰零伍 rather than 壹佰零伍零.
func sectionText(group int64, units []string) string {
	digits := []int{}
	for group > 0 {
		digits = append([]int{int(group % 10)}, digits...)
		group /= 10
	}
	// Drop leading zeros (the highest positions) but keep interior and
	// trailing ones: 105 must keep its final 5.
	for len(digits) > 0 && digits[0] == 0 {
		digits = digits[1:]
	}
	var b strings.Builder
	zero := false
	for i, d := range digits {
		pos := len(digits) - i - 1
		switch {
		case d == 0:
			zero = true
		case zero:
			// A zero digit was skipped before this one, so emit the
			// placeholder and then this digit with its unit.
			b.WriteString("零")
			b.WriteString(chineseDigits[d])
			b.WriteString(units[pos])
			zero = false
		default:
			b.WriteString(chineseDigits[d])
			b.WriteString(units[pos])
		}
	}
	return b.String()
}

// collapseZeros merges consecutive "零" characters into one.
func collapseZeros(s string) string {
	var b strings.Builder
	prevZero := false
	for _, r := range s {
		if r == '零' {
			if prevZero {
				continue
			}
			prevZero = true
		} else {
			prevZero = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func flatten(vals []any) []any {
	var out []any
	for _, v := range vals {
		if vec, ok := v.([]any); ok {
			out = append(out, vec...)
		} else {
			out = append(out, v)
		}
	}
	return out
}

func numbers(vals []any) []float64 {
	var out []float64
	for _, v := range flatten(vals) {
		if f, ok := toFloat(v); ok {
			out = append(out, f)
		}
	}
	return out
}

func sumAndCount(vals []any) (float64, int) {
	sum, n := 0.0, 0
	for _, v := range flatten(vals) {
		if f, ok := toFloat(v); ok {
			sum += f
			n++
		}
	}
	return sum, n
}

func sumValues(vals []any) any {
	s, _ := sumAndCount(vals)
	return s
}

func scalar(v any) any {
	if vec, ok := v.([]any); ok {
		if len(vec) == 0 {
			return nil
		}
		return vec[0]
	}
	return v
}

func vectorMap(v any, f func(any) (any, error)) (any, error) {
	if vec, ok := v.([]any); ok {
		out := make([]any, len(vec))
		for i := range vec {
			var err error
			out[i], err = f(vec[i])
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return f(v)
}

func vectorMapMany(vals []any, f func([]any) (any, error)) (any, error) {
	n := 1
	for _, v := range vals {
		if vec, ok := v.([]any); ok && len(vec) > n {
			n = len(vec)
		}
	}
	if n == 1 {
		items := make([]any, len(vals))
		for i, v := range vals {
			items[i] = scalar(v)
		}
		return f(items)
	}
	out := make([]any, n)
	for i := 0; i < n; i++ {
		items := make([]any, len(vals))
		for j, v := range vals {
			if vec, ok := v.([]any); ok {
				items[j] = elemAt(vec, i)
			} else {
				items[j] = v
			}
		}
		var err error
		out[i], err = f(items)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && !strings.EqualFold(t, "false")
	case float64:
		return t != 0
	case int64:
		return t != 0
	}
	return true
}

func stringOf(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case []any:
		if len(t) == 1 {
			return stringOf(t[0])
		}
		parts := make([]string, len(t))
		for i := range t {
			parts[i] = stringOf(t[i])
		}
		return strings.Join(parts, ",")
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case time.Time:
		return t.Format("2006-01-02")
	}
	return plainValue(v)
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, false
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		s = strings.ReplaceAll(s, ",", "")
		s = strings.ReplaceAll(s, "，", "")
		pct := strings.HasSuffix(s, "%")
		s = strings.TrimSuffix(s, "%")
		s = strings.TrimPrefix(s, "¥")
		s = strings.TrimPrefix(s, "￥")
		s = strings.TrimPrefix(s, "$")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		if pct {
			f /= 100
		}
		return f, true
	}
	return 0, false
}

func toTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		s := strings.TrimSpace(t)
		layouts := []string{
			"2006-01-02 15:04:05", "2006-01-02", "2006/01/02 15:04:05", "2006/01/02",
			"2006年01月02日", "2006年1月2日", "20060102", "01/02/2006",
			time.RFC3339, "2006-01-02T15:04:05",
		}
		for _, l := range layouts {
			if tt, err := time.ParseInLocation(l, s, time.Local); err == nil {
				return tt, true
			}
		}
	}
	return time.Time{}, false
}

// ---- tokenizer ----

type tokenKind int

const (
	tkEOF tokenKind = iota
	tkNumber
	tkString
	tkIdent
	tkParam
	tkOp
	tkLParen
	tkRParen
	tkComma
	tkColon
	tkLBracket
	tkRBracket
	tkLBrace
	tkRBrace
	tkBang
	tkSemi
	tkColonPlus
	tkSheet
	tkDot
)

type token struct {
	kind tokenKind
	text string
	num  float64
}

func lexFormula(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			start := i
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
				j := i + 1
				if j < len(s) && (s[j] == '+' || s[j] == '-') {
					j++
				}
				if j < len(s) && s[j] >= '0' && s[j] <= '9' {
					i = j
					for i < len(s) && s[i] >= '0' && s[i] <= '9' {
						i++
					}
				}
			}
			f, err := strconv.ParseFloat(s[start:i], 64)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tkNumber, text: s[start:i], num: f})
		case c == '"' || c == '\'':
			quote := c
			i++
			var b strings.Builder
			closed := false
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				if s[i] == quote {
					if i+1 < len(s) && s[i+1] == quote {
						b.WriteByte(quote)
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				b.WriteByte(s[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("字符串未闭合")
			}
			// A single-quoted name followed by "!" is a cross-sheet
			// reference such as '2010-实际'!N5.
			if quote == '\'' && i < len(s) && s[i] == '!' {
				toks = append(toks, token{kind: tkSheet, text: b.String()})
				i++
				continue
			}
			toks = append(toks, token{kind: tkString, text: b.String()})
		case c == '$':
			// FineReport writes a parameter either as $name or as ${name}. The
			// braced form is the canonical one; the bare form is kept for
			// compatibility with templates that omit the braces.
			if i+1 < len(s) && s[i+1] == '{' {
				j := i + 2
				for j < len(s) && s[j] != '}' {
					j++
				}
				if j >= len(s) {
					return nil, fmt.Errorf("参数占位符未闭合: %s", s[i:])
				}
				name := s[i+2 : j]
				if name == "" {
					return nil, fmt.Errorf("参数占位符为空")
				}
				toks = append(toks, token{kind: tkParam, text: name})
				i = j + 1
				continue
			}
			start := i
			i++
			for i < len(s) && (isIdentByte(s[i]) || s[i] == '_') {
				i++
			}
			// $A$1 style absolute cell references keep their dollar signs.
			if i < len(s) && s[i] == '$' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
				i++
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				toks = append(toks, token{kind: tkIdent, text: strings.ToUpper(s[start:i])})
				continue
			}
			word := s[start:i]
			if isCellRef(word) {
				toks = append(toks, token{kind: tkIdent, text: strings.ToUpper(word)})
			} else {
				toks = append(toks, token{kind: tkParam, text: strings.TrimPrefix(word, "$")})
			}
		case isIdentStart(c) || c == '_':
			start := i
			for i < len(s) && (isIdentByte(s[i]) || s[i] == '_') {
				i++
			}
			word := s[start:i]
			toks = append(toks, token{kind: tkIdent, text: word})
		case c == '(':
			toks = append(toks, token{kind: tkLParen, text: "("})
			i++
		case c == ')':
			toks = append(toks, token{kind: tkRParen, text: ")"})
			i++
		case c == '[':
			toks = append(toks, token{kind: tkLBracket, text: "["})
			i++
		case c == ']':
			toks = append(toks, token{kind: tkRBracket, text: "]"})
			i++
		case c == '{':
			toks = append(toks, token{kind: tkLBrace, text: "{"})
			i++
		case c == '}':
			toks = append(toks, token{kind: tkRBrace, text: "}"})
			i++
		case c == '!':
			toks = append(toks, token{kind: tkBang, text: "!"})
			i++
		case c == ';':
			toks = append(toks, token{kind: tkSemi, text: ";"})
			i++
		case c == ',':
			toks = append(toks, token{kind: tkComma, text: ","})
			i++
		case c == ':':
			toks = append(toks, token{kind: tkColon, text: ":"})
			i++
		case c == '.':
			// A dot after an identifier starts a method call such as
			// ds1.select(销售额). A leading dot is a decimal point and is
			// handled by the number branch above.
			if len(toks) > 0 && toks[len(toks)-1].kind == tkIdent {
				toks = append(toks, token{kind: tkDot, text: "."})
				i++
				continue
			}
			return nil, fmt.Errorf("无法识别的字符 '.'")
		default:
			op := ""
			if i+1 < len(s) {
				two := s[i : i+2]
				switch two {
				case "<=", ">=", "<>", "!=", "==":
					op = two
				}
			}
			if op == "" {
				switch c {
				case '+', '-', '*', '/', '%', '&', '=', '<', '>', '^':
					op = string(c)
				}
			}
			if op == "" {
				return nil, fmt.Errorf("无法识别的字符 %q", c)
			}
			toks = append(toks, token{kind: tkOp, text: op})
			i += len(op)
		}
	}
	toks = append(toks, token{kind: tkEOF})
	return toks, nil
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isIdentStart reports whether a byte can begin an identifier. Bytes with the
// high bit set are UTF-8 continuation/lead bytes, which lets Chinese
// identifiers (parameter names such as $销售员) lex correctly.
func isIdentStart(c byte) bool {
	return isLetter(c) || c >= 0x80
}

// isIdentByte reports whether a byte may continue an identifier.
func isIdentByte(c byte) bool {
	return isLetter(c) || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}

func isCellRef(s string) bool {
	t := strings.TrimPrefix(s, "$")
	j := 0
	for j < len(t) && isLetter(t[j]) {
		j++
	}
	if j == 0 || j > 3 || j == len(t) {
		return false
	}
	if t[j] == '$' {
		j++
	}
	if j >= len(t) {
		return false
	}
	for ; j < len(t); j++ {
		if t[j] < '0' || t[j] > '9' {
			return false
		}
	}
	return true
}

// ---- parser ----

type formulaParser struct {
	toks []token
	pos  int
}

func (p *formulaParser) peek() token { return p.toks[p.pos] }
func (p *formulaParser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *formulaParser) parse() (formulaExpr, error) {
	e, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkEOF {
		return nil, fmt.Errorf("公式存在多余内容: %s", p.peek().text)
	}
	return e, nil
}

func (p *formulaParser) parseComparison() (formulaExpr, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tkOp {
			return left, nil
		}
		switch t.text {
		case "=", "==", "<>", "!=", "<", ">", "<=", ">=":
			p.next()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = binExpr{op: t.text, l: left, r: right}
		default:
			return left, nil
		}
	}
}

func (p *formulaParser) parseConcat() (formulaExpr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkOp && p.peek().text == "&" {
		p.next()
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = binExpr{op: "&", l: left, r: right}
	}
	return left, nil
}

func (p *formulaParser) parseAdditive() (formulaExpr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkOp && (p.peek().text == "+" || p.peek().text == "-") {
		op := p.next().text
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = binExpr{op: op, l: left, r: right}
	}
	return left, nil
}

func (p *formulaParser) parseMultiplicative() (formulaExpr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkOp {
		op := p.peek().text
		if op != "*" && op != "/" && op != "%" && op != "^" {
			break
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binExpr{op: op, l: left, r: right}
	}
	return left, nil
}

func (p *formulaParser) parseUnary() (formulaExpr, error) {
	if p.peek().kind == tkOp && (p.peek().text == "-" || p.peek().text == "+" || p.peek().text == "&") {
		op := p.next().text
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: op, x: x}, nil
	}
	if p.peek().kind == tkIdent && strings.EqualFold(p.peek().text, "not") {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: "not", x: x}, nil
	}
	return p.parsePrimary()
}

// parseArrayLiteral parses FineReport's "[a, b, c]A" array literal. The
// trailing "A" type marker is optional and simply consumed.
func (p *formulaParser) parseArrayLiteral() (formulaExpr, error) {
	var items []formulaExpr
	if p.peek().kind != tkRBracket {
		for {
			item, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
			if p.peek().kind == tkComma {
				p.next()
				continue
			}
			break
		}
	}
	if p.peek().kind != tkRBracket {
		return nil, fmt.Errorf("数组字面量缺少右方括号")
	}
	p.next()
	// Consume the optional "A" type marker that follows the literal.
	if p.peek().kind == tkIdent && strings.EqualFold(p.peek().text, "A") {
		p.next()
	}
	return arrayExpr{items: items}, nil
}

// parseSetLiteral parses FineReport's "{a, b}" set literal.
func (p *formulaParser) parseSetLiteral() (formulaExpr, error) {
	var items []formulaExpr
	if p.peek().kind != tkRBrace {
		for {
			item, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
			if p.peek().kind == tkComma {
				p.next()
				continue
			}
			break
		}
	}
	if p.peek().kind != tkRBrace {
		return nil, fmt.Errorf("集合字面量缺少右花括号")
	}
	p.next()
	return setExpr{items: items}, nil
}

func (p *formulaParser) parsePrimary() (formulaExpr, error) {
	t := p.next()
	switch t.kind {
	case tkNumber:
		return litExpr{v: t.num}, nil
	case tkString:
		return litExpr{v: t.text}, nil
	case tkParam:
		return paramExpr{name: t.text}, nil
	case tkLBracket:
		return p.parseArrayLiteral()
	case tkLBrace:
		return p.parseSetLiteral()
	case tkSheet:
		// Cross-sheet reference: '2010-实际'!N5
		if p.peek().kind != tkIdent || !isCellRef(p.peek().text) {
			return nil, fmt.Errorf("跨表引用的单元格地址无效")
		}
		cellTok := p.next()
		return sheetRefExpr{sheet: t.text, ref: strings.ToUpper(cellTok.text)}, nil
	case tkLParen:
		e, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tkRParen {
			return nil, fmt.Errorf("缺少右括号")
		}
		p.next()
		return e, nil
	case tkIdent:
		// A method call such as ds1.select(销售额) or ds1.group(2011).
		if p.peek().kind == tkDot {
			p.next()
			method := p.next()
			if method.kind != tkIdent {
				return nil, fmt.Errorf("方法名无效")
			}
			if p.peek().kind != tkLParen {
				return nil, fmt.Errorf("方法 %s 缺少左括号", method.text)
			}
			p.next()
			call := methodCallExpr{receiver: t.text, method: method.text}
			if p.peek().kind == tkRParen {
				p.next()
				return call, nil
			}
			for {
				arg, err := p.parseComparison()
				if err != nil {
					return nil, err
				}
				call.args = append(call.args, arg)
				if p.peek().kind == tkComma {
					p.next()
					continue
				}
				break
			}
			if p.peek().kind != tkRParen {
				return nil, fmt.Errorf("方法 %s 缺少右括号", method.text)
			}
			p.next()
			return call, nil
		}
		if p.peek().kind == tkLParen {
			p.next()
			call := callExpr{name: t.text}
			if p.peek().kind == tkRParen {
				p.next()
				return call, nil
			}
			for {
				arg, err := p.parseComparison()
				if err != nil {
					return nil, err
				}
				call.args = append(call.args, arg)
				if p.peek().kind == tkComma {
					p.next()
					continue
				}
				break
			}
			if p.peek().kind != tkRParen {
				return nil, fmt.Errorf("函数 %s 缺少右括号", t.text)
			}
			p.next()
			return call, nil
		}
		lower := strings.ToLower(t.text)
		switch lower {
		case "true":
			return litExpr{v: true}, nil
		case "false":
			return litExpr{v: false}, nil
		case "null":
			return litExpr{v: nil}, nil
		}
		ref := strings.ToUpper(t.text)
		if p.peek().kind == tkColon {
			p.next()
			endTok := p.next()
			if endTok.kind != tkIdent || !isCellRef(endTok.text) {
				return nil, fmt.Errorf("区域结束位置无效")
			}
			return rangeExpr{start: ref, end: strings.ToUpper(endTok.text)}, nil
		}
		if isCellRef(ref) {
			return refExpr{ref: ref}, nil
		}
		return identExpr{name: t.text}, nil
	}
	return nil, fmt.Errorf("公式语法错误，意外的 %q", t.text)
}

// EvalFormula parses and evaluates a FineReport formula without the leading '='.
func EvalFormula(formula string, ctx *EvalContext) (any, error) {
	s := strings.TrimSpace(formula)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "=") {
		s = s[1:]
	}
	if s == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = NewEvalContext()
	}
	toks, err := lexFormula(s)
	if err != nil {
		return nil, err
	}
	p := &formulaParser{toks: toks}
	expr, err := p.parse()
	if err != nil {
		return nil, err
	}
	v, err := expr.eval(ctx)
	if err != nil {
		return nil, err
	}
	if vec, ok := v.([]any); ok && len(vec) == 1 {
		return vec[0], nil
	}
	return v, nil
}

// CellRefParts splits A1/AA23 into zero-based column and row indexes.
func CellRefParts(ref string) (col, row int, ok bool) {
	s := strings.ReplaceAll(ref, "$", "")
	s = strings.ToUpper(strings.TrimSpace(s))
	j := 0
	col = 0
	for j < len(s) && s[j] >= 'A' && s[j] <= 'Z' {
		col = col*26 + int(s[j]-'A'+1)
		j++
	}
	if j == 0 || j >= len(s) {
		return 0, 0, false
	}
	r, err := strconv.Atoi(s[j:])
	if err != nil || r < 1 {
		return 0, 0, false
	}
	return col - 1, r - 1, true
}

package fr

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// evalOK evaluates a formula that must succeed and returns the result.
func evalOK(t *testing.T, formula string, ctx *EvalContext) any {
	t.Helper()
	if ctx == nil {
		ctx = NewEvalContext()
	}
	v, err := EvalFormula(formula, ctx)
	if err != nil {
		t.Fatalf("EvalFormula(%q) failed: %v", formula, err)
	}
	return v
}

// evalNum evaluates a formula that must produce a number.
func evalNum(t *testing.T, formula string, ctx *EvalContext) float64 {
	t.Helper()
	v := evalOK(t, formula, ctx)
	f, ok := toFloat(v)
	if !ok {
		t.Fatalf("EvalFormula(%q) = %#v, want number", formula, v)
	}
	return f
}

func TestFormulaArithmetic(t *testing.T) {
	cases := []struct {
		formula string
		want    float64
	}{
		{"1+2", 3},
		{"10-4", 6},
		{"3*4", 12},
		{"10/4", 2.5},
		{"7%3", 1},
		{"2^10", 1024},
		{"-5+2", -3},
		{"(1+2)*3", 9},
		{"1+2*3", 7},
		{"1=1", 1},
		{"1<>1", 0},
		{"2>1", 1},
		{"2<=1", 0},
	}
	for _, c := range cases {
		if got := evalNum(t, c.formula, nil); got != c.want {
			t.Errorf("%s = %v, want %v", c.formula, got, c.want)
		}
	}
	// String concatenation is checked separately because it yields text.
	if v := evalOK(t, "\"a\"&\"b\"", nil); v != "ab" {
		t.Errorf("concat = %#v", v)
	}
}

func TestFormulaStringConcat(t *testing.T) {
	v := evalOK(t, "\"总\"&\"计\"&\"=\"&100", nil)
	if v != "总计=100" {
		t.Errorf("concat = %#v", v)
	}
	// The + operator concatenates when a non-numeric string is involved.
	v = evalOK(t, "\"a\"+\"b\"", nil)
	if v != "ab" {
		t.Errorf("plus concat = %#v", v)
	}
}

func TestFormulaAggregateFunctions(t *testing.T) {
	cases := []struct {
		formula string
		want    any
	}{
		{"SUM(1,2,3)", float64(6)},
		{"AVERAGE(2,4)", float64(3)},
		{"AVG(2,4)", float64(3)},
		{"COUNT(1,2,3)", float64(3)},
		{"MAX(1,9,3)", float64(9)},
		{"MIN(1,9,3)", float64(1)},
		{"ROUND(3.14159, 2)", 3.14},
		{"ROUNDUP(3.14159, 2)", 3.15},
		{"ROUNDDOWN(3.14159, 2)", 3.14},
		{"INT(3.9)", float64(3)},
		{"ABS(-5)", float64(5)},
		{"SIGN(-2)", float64(-1)},
		{"SQRT(16)", float64(4)},
		{"MOD(10,3)", float64(1)},
		{"POWER(2,8)", float64(256)},
		{"LEN(\"abc\")", float64(3)},
		{"LEFT(\"abcdef\",3)", "abc"},
		{"RIGHT(\"abcdef\",3)", "def"},
		{"MID(\"abcdef\",2,3)", "bcd"},
		{"TRIM(\"  x  \")", "x"},
		{"UPPER(\"ab\")", "AB"},
		{"LOWER(\"AB\")", "ab"},
		{"VALUE(\"12.5\")", 12.5},
	}
	for _, c := range cases {
		v := evalOK(t, c.formula, nil)
		switch want := c.want.(type) {
		case string:
			if v != want {
				t.Errorf("%s = %#v, want %q", c.formula, v, want)
			}
		case float64:
			f, ok := toFloat(v)
			if !ok || f != want {
				t.Errorf("%s = %#v, want %v", c.formula, v, want)
			}
		default:
			t.Errorf("%s: unsupported expectation type %T", c.formula, c.want)
		}
	}
}

func TestFormulaLogicFunctions(t *testing.T) {
	if v := evalOK(t, "IF(1=1, \"yes\", \"no\")", nil); v != "yes" {
		t.Errorf("IF true = %#v", v)
	}
	if v := evalOK(t, "IF(1=2, \"yes\", \"no\")", nil); v != "no" {
		t.Errorf("IF false = %#v", v)
	}
	// IF must not evaluate the branch that is not taken.
	if v := evalOK(t, "IF(1=1, 1, 1/0)", nil); v != float64(1) {
		t.Errorf("IF short circuit = %#v", v)
	}
	if v := evalOK(t, "AND(1=1, 2=2)", nil); v != true {
		t.Errorf("AND = %#v", v)
	}
	if v := evalOK(t, "OR(1=2, 2=2)", nil); v != true {
		t.Errorf("OR = %#v", v)
	}
	if v := evalOK(t, "NOT(1=1)", nil); v != false {
		t.Errorf("NOT = %#v", v)
	}
	if v := evalOK(t, "SWITCH(2, 1, \"a\", 2, \"b\", \"z\")", nil); v != "b" {
		t.Errorf("SWITCH = %#v", v)
	}
	if v := evalOK(t, "ISNULL(\"\")", nil); v != true {
		t.Errorf("ISNULL = %#v", v)
	}
}

func TestFormulaIfError(t *testing.T) {
	// The first argument fails (non-numeric operand), so the fallback is used.
	if v := evalOK(t, "IFERROR(\"abc\" - 1, 99)", nil); v != float64(99) {
		t.Errorf("IFERROR fallback = %#v", v)
	}
	// A successful first argument passes straight through.
	if v := evalOK(t, "IFERROR(1, 99)", nil); v != float64(1) {
		t.Errorf("IFERROR passthrough = %#v", v)
	}
	// A failing cell reference is caught as well.
	ctx := NewEvalContext()
	ctx.Ref = func(string) (any, bool) { return nil, false }
	if v := evalOK(t, "IFERROR(A1, \"missing\")", ctx); v != "missing" {
		t.Errorf("IFERROR ref = %#v", v)
	}
}

func TestFormulaDivisionByZeroBlank(t *testing.T) {
	// Division by an empty or zero divisor yields a blank cell, matching
	// FineReport, not a #DIV/0 error.
	if v := evalOK(t, "1/0", nil); v != nil {
		t.Errorf("1/0 = %#v, want blank", v)
	}
	ctx := NewEvalContext()
	ctx.Ref = func(ref string) (any, bool) {
		if ref == "A1" {
			return nil, true // empty cell
		}
		return 0.0, true
	}
	if v := evalOK(t, "A1/B1", ctx); v != nil {
		t.Errorf("A1/B1 = %#v, want blank", v)
	}
}

func TestFormulaCellReferences(t *testing.T) {
	ctx := NewEvalContext()
	ctx.Ref = func(ref string) (any, bool) {
		switch ref {
		case "A1":
			return float64(10), true
		case "B2":
			return float64(4), true
		}
		return nil, false
	}
	if got := evalNum(t, "A1+B2", ctx); got != 14 {
		t.Errorf("A1+B2 = %v, want 14", got)
	}
	if got := evalNum(t, "A1*2", ctx); got != 20 {
		t.Errorf("A1*2 = %v, want 20", got)
	}
	// An unresolvable reference must be an error, not a silent zero.
	if _, err := EvalFormula("Z9+1", ctx); err == nil {
		t.Error("expected unknown cell to fail")
	}
}

func TestFormulaRangeReference(t *testing.T) {
	ctx := NewEvalContext()
	ctx.Range = func(a, b string) (any, bool) {
		return []any{float64(1), float64(2), float64(3)}, true
	}
	if got := evalNum(t, "SUM(A1:A3)", ctx); got != 6 {
		t.Errorf("SUM(A1:A3) = %v, want 6", got)
	}
}

func TestFormulaParameters(t *testing.T) {
	ctx := NewEvalContext()
	ctx.Params = map[string]any{"year": "2024", "销售员": "张三"}
	if v := evalOK(t, "$year", ctx); v != "2024" {
		t.Errorf("$year = %#v", v)
	}
	// Chinese parameter names must lex correctly.
	if v := evalOK(t, "$销售员", ctx); v != "张三" {
		t.Errorf("$销售员 = %#v", v)
	}
	// The braced form is the canonical FineReport syntax.
	if v := evalOK(t, "${year}", ctx); v != "2024" {
		t.Errorf("${year} = %#v", v)
	}
	if v := evalOK(t, "${销售员}", ctx); v != "张三" {
		t.Errorf("${销售员} = %#v", v)
	}
	if got := evalNum(t, "LEN($销售员)", ctx); got != 2 {
		t.Errorf("LEN($销售员) = %v, want 2", got)
	}
	// An unclosed placeholder is a syntax error.
	if _, err := EvalFormula("${year", ctx); err == nil {
		t.Error("expected unclosed placeholder to fail")
	}
}

func TestFormulaArrayAndSetLiterals(t *testing.T) {
	v := evalOK(t, "[1,2,3]A", nil)
	vec, ok := v.([]any)
	if !ok || len(vec) != 3 {
		t.Fatalf("array literal = %#v", v)
	}
	if vec[0] != float64(1) || vec[2] != float64(3) {
		t.Errorf("array literal = %#v", vec)
	}
	// A set literal drops duplicates.
	v = evalOK(t, "{1,2,2,3}", nil)
	vec, ok = v.([]any)
	if !ok || len(vec) != 3 {
		t.Fatalf("set literal = %#v", v)
	}
}

func TestFormulaStringHelpers(t *testing.T) {
	cases := []struct {
		formula string
		want    any
	}{
		{"SPLIT(\"a,b,c\", \",\")", []any{"a", "b", "c"}},
		{"REPLACE(\"a-b-c\", \"-\", \"+\")", "a+b+c"},
		{"FIND(\".\", \"a.b.c\")", float64(2)},
		{"FIND(\"z\", \"abc\")", float64(0)},
		{"REPEAT(\"|\", 3)", "|||"},
		{"CONCATENATE(\"a\",\"b\",\"c\")", "abc"},
	}
	for _, c := range cases {
		v := evalOK(t, c.formula, nil)
		switch want := c.want.(type) {
		case string:
			if v != want {
				t.Errorf("%s = %#v, want %q", c.formula, v, want)
			}
		case []any:
			vec, ok := v.([]any)
			if !ok || len(vec) != len(want) {
				t.Errorf("%s = %#v, want %v", c.formula, v, want)
				continue
			}
			for i := range want {
				if vec[i] != want[i] {
					t.Errorf("%s = %#v, want %v", c.formula, vec, want)
					break
				}
			}
		case float64:
			f, ok := toFloat(v)
			if !ok || f != want {
				t.Errorf("%s = %#v, want %v", c.formula, v, want)
			}
		default:
			t.Errorf("%s: unsupported expectation type %T", c.formula, c.want)
		}	}
}

func TestFormulaArrayFunctions(t *testing.T) {
	if got := evalNum(t, "INARRAY(\"b\", [\"a\",\"b\",\"c\"]A)", nil); got != 2 {
		t.Errorf("INARRAY = %v, want 2", got)
	}
	if got := evalNum(t, "INARRAY(\"z\", [\"a\",\"b\"]A)", nil); got != 0 {
		t.Errorf("INARRAY missing = %v, want 0", got)
	}
	if got := evalNum(t, "INDEXOFARRAY(\"c\", [\"a\",\"b\",\"c\"]A)", nil); got != 3 {
		t.Errorf("INDEXOFARRAY = %v, want 3", got)
	}
	if v := evalOK(t, "JOINARRAY([\"a\",\"b\"], \"-\")", nil); v != "a-b" {
		t.Errorf("JOINARRAY = %#v", v)
	}
	v := evalOK(t, "UNIQUEARRAY([1,1,2]A)", nil)
	vec, _ := v.([]any)
	if len(vec) != 2 {
		t.Errorf("UNIQUEARRAY = %#v", v)
	}
	v = evalOK(t, "SORT([3,1,2]A)", nil)
	vec, _ = v.([]any)
	if len(vec) != 3 || vec[0] != float64(1) {
		t.Errorf("SORT = %#v", v)
	}
	v = evalOK(t, "SORT([3,1,2]A, true)", nil)
	vec, _ = v.([]any)
	if len(vec) != 3 || vec[0] != float64(3) {
		t.Errorf("SORT desc = %#v", v)
	}
}

func TestFormulaDateFunctions(t *testing.T) {
	ctx := NewEvalContext()
	ctx.Now = time.Date(2026, 3, 15, 10, 30, 0, 0, time.Local)
	if got := evalNum(t, "YEAR(TODAY())", ctx); got != 2026 {
		t.Errorf("YEAR = %v, want 2026", got)
	}
	if got := evalNum(t, "MONTH(TODAY())", ctx); got != 3 {
		t.Errorf("MONTH = %v, want 3", got)
	}
	if got := evalNum(t, "DAY(TODAY())", ctx); got != 15 {
		t.Errorf("DAY = %v, want 15", got)
	}
	// DATEDIF counts whole days between two dates.
	if got := evalNum(t, "DATEDIF(DATE(2026,1,1), DATE(2026,1,11), \"D\")", ctx); got != 10 {
		t.Errorf("DATEDIF D = %v, want 10", got)
	}
	if got := evalNum(t, "DATEDIF(DATE(2026,1,1), DATE(2027,3,1), \"M\")", ctx); got != 14 {
		t.Errorf("DATEDIF M = %v, want 14", got)
	}
	if got := evalNum(t, "DATEDIF(DATE(2026,1,1), DATE(2027,3,1), \"Y\")", ctx); got != 1 {
		t.Errorf("DATEDIF Y = %v, want 1", got)
	}
	// DATEDELTA shifts by days, MONTHDELTA by months.
	v := evalOK(t, "DATEDELTA(DATE(2026,3,15), -3)", ctx)
	if ts, ok := v.(time.Time); !ok || ts.Day() != 12 {
		t.Errorf("DATEDELTA = %#v", v)
	}
	v = evalOK(t, "MONTHDELTA(DATE(2026,3,15), -1)", ctx)
	if ts, ok := v.(time.Time); !ok || ts.Month() != time.February {
		t.Errorf("MONTHDELTA = %#v", v)
	}
	// DATEINMONTH snaps to the first day of the month.
	v = evalOK(t, "DATEINMONTH(DATE(2026,3,15), -1)", ctx)
	if ts, ok := v.(time.Time); !ok || ts.Day() != 1 || ts.Month() != time.February {
		t.Errorf("DATEINMONTH = %#v", v)
	}
}

func TestFormulaRandomFunctions(t *testing.T) {
	for i := 0; i < 20; i++ {
		v := evalNum(t, "RANDBETWEEN(100, 200)", nil)
		if v < 100 || v > 200 {
			t.Fatalf("RANDBETWEEN out of range: %v", v)
		}
	}
	v := evalOK(t, "RAND()", nil)
	f, ok := toFloat(v)
	if !ok || f < 0 || f >= 1 {
		t.Errorf("RAND = %#v", v)
	}
}

func TestFormulaCNMONEY(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "零元整"},
		{1, "壹元整"},
		{10, "壹拾元整"},
		{105, "壹佰零伍元整"},
		{1234.56, "壹仟贰佰叁拾肆元伍角陆分"},
	}
	for _, c := range cases {
		got := chineseMoney(c.in)
		if got != c.want {
			t.Errorf("chineseMoney(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormulaUnknownFunction(t *testing.T) {
	_, err := EvalFormula("NOSUCHFUNC(1)", NewEvalContext())
	if err == nil {
		t.Fatal("expected unknown function to fail")
	}
	if !strings.Contains(err.Error(), "暂不支持") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFormulaSyntaxError(t *testing.T) {
	for _, f := range []string{"1 +", "(1+2", "SUM(1,", "1 @ 2"} {
		if _, err := EvalFormula(f, NewEvalContext()); err == nil {
			t.Errorf("expected %q to fail", f)
		}
	}
}

func TestFormulaLeadingEquals(t *testing.T) {
	// FineReport stores formulas with a leading "=".
	if got := evalNum(t, "=1+2", nil); got != 3 {
		t.Errorf("=1+2 = %v, want 3", got)
	}
	if got := evalNum(t, "  =1+2  ", nil); got != 3 {
		t.Errorf("padded =1+2 = %v, want 3", got)
	}
}

func TestFormulaVectorArithmetic(t *testing.T) {
	ctx := NewEvalContext()
	ctx.Range = func(a, b string) (any, bool) {
		return []any{float64(1), float64(2), float64(3)}, true
	}
	v := evalOK(t, "A1:A3 * 2", ctx)
	vec, ok := v.([]any)
	if !ok || len(vec) != 3 {
		t.Fatalf("vector = %#v", v)
	}
	if vec[2] != float64(6) {
		t.Errorf("vector = %#v", vec)
	}
}

func TestFormulaGrepAndMapArray(t *testing.T) {
	// GREPARRAY keeps the elements whose condition holds. The condition sees
	// the current element through the "item" variable.
	v := evalOK(t, "GREPARRAY([1,2,3,4]A, item > 2)", nil)
	vec, _ := v.([]any)
	if len(vec) != 2 || vec[0] != float64(3) || vec[1] != float64(4) {
		t.Errorf("GREPARRAY = %#v", v)
	}
	// MAPARRAY transforms each element.
	v = evalOK(t, "MAPARRAY([1,2,3]A, item * 2)", nil)
	vec, _ = v.([]any)
	if len(vec) != 3 || vec[2] != float64(6) {
		t.Errorf("MAPARRAY = %#v", v)
	}
	// A function call on the bound item works too: LEFT("ab", item) has a
	// length of 1 for item=1 and 2 for item=2.
	v = evalOK(t, "MAPARRAY([1,2]A, LEN(LEFT(\"ab\", item)))", nil)
	vec, _ = v.([]any)
	if len(vec) != 2 || vec[0] != float64(1) || vec[1] != float64(2) {
		t.Errorf("MAPARRAY with function = %#v", v)
	}
}

func TestFormulaRangeAndArrayBuilders(t *testing.T) {
	v := evalOK(t, "RANGE(1,5)", nil)
	vec, _ := v.([]any)
	if len(vec) != 5 || vec[4] != float64(5) {
		t.Errorf("RANGE = %#v", v)
	}
	v = evalOK(t, "ARRAY(1,2,3)", nil)
	vec, _ = v.([]any)
	if len(vec) != 3 {
		t.Errorf("ARRAY = %#v", v)
	}
	v = evalOK(t, "REVERSEARRAY([1,2,3]A)", nil)
	vec, _ = v.([]any)
	if len(vec) != 3 || vec[0] != float64(3) {
		t.Errorf("REVERSEARRAY = %#v", v)
	}
}

func TestFormulaValueCrossReport(t *testing.T) {
	ctx := NewEvalContext()
	ctx.ReportCell = func(report string, row, col int) (any, bool) {
		if report == "账务基础表" && row == 2 && col == 1 {
			return float64(1234), true
		}
		return nil, false
	}
	got := evalNum(t, "VALUE(\"账务基础表\", 2, 1)", ctx)
	if got != 1234 {
		t.Errorf("VALUE cross report = %v, want 1234", got)
	}
	// A missing report cell must be an error rather than a silent zero.
	if _, err := EvalFormula("VALUE(\"不存在\", 1, 1)", ctx); err == nil {
		t.Error("expected missing report cell to fail")
	}
	// The single argument form still converts text to a number.
	if got := evalNum(t, "VALUE(\"12.5\")", nil); got != 12.5 {
		t.Errorf("VALUE text = %v, want 12.5", got)
	}
}

func TestFormulaTableDataFields(t *testing.T) {
	ctx := NewEvalContext()
	ctx.DatasetFields = func(name string) ([]string, bool) {
		if name == "ds1" {
			return []string{"金额", "日期"}, true
		}
		return nil, false
	}
	v := evalOK(t, "TABLEDATAFIELDS(\"ds1\")", ctx)
	vec, _ := v.([]any)
	if len(vec) != 2 || vec[0] != "金额" {
		t.Errorf("TABLEDATAFIELDS = %#v", v)
	}
	if _, err := EvalFormula("TABLEDATAFIELDS(\"nope\")", ctx); err == nil {
		t.Error("expected unknown dataset to fail")
	}
}

func TestFormulaDatasetSelect(t *testing.T) {
	ctx := NewEvalContext()
	ctx.DatasetColumnValues = func(dataset, column string) ([]any, bool) {
		switch dataset + "." + column {
		case "ds1.销售额":
			return []any{float64(100), float64(200), float64(300)}, true
		case "ds1.公司名称":
			return []any{"甲公司", "乙公司", "甲公司"}, true
		}
		return nil, false
	}
	// select returns every value of the column.
	v := evalOK(t, "ds1.select(销售额)", ctx)
	vec, ok := v.([]any)
	if !ok || len(vec) != 3 {
		t.Fatalf("select = %#v", v)
	}
	if vec[2] != float64(300) {
		t.Errorf("select = %#v", vec)
	}
	// The values feed into the usual aggregate functions.
	if got := evalNum(t, "SUM(ds1.select(销售额))", ctx); got != 600 {
		t.Errorf("SUM(select) = %v, want 600", got)
	}
	if got := evalNum(t, "MAX(ds1.select(销售额))", ctx); got != 300 {
		t.Errorf("MAX(select) = %v, want 300", got)
	}
	// A second argument filters the column by another column's value.
	v = evalOK(t, "ds1.select(销售额, 公司名称=\"甲公司\")", ctx)
	vec, _ = v.([]any)
	if len(vec) != 2 {
		t.Errorf("filtered select = %#v", v)
	}
	// An unknown column is an error.
	if _, err := EvalFormula("ds1.select(不存在)", ctx); err == nil {
		t.Error("expected unknown column to fail")
	}
}

func TestFormulaDatasetGroup(t *testing.T) {
	ctx := NewEvalContext()
	ctx.DatasetGroup = func(dataset string, key any) (any, bool) {
		if dataset == "ds1" {
			return float64(42), true
		}
		return nil, false
	}
	if got := evalNum(t, "ds1.group(2011)", ctx); got != 42 {
		t.Errorf("group = %v, want 42", got)
	}
}

func TestFormulaUnknownDatasetMethod(t *testing.T) {
	_, err := EvalFormula("ds1.nosuch(1)", NewEvalContext())
	if err == nil {
		t.Fatal("expected unknown dataset method to fail")
	}
	if !strings.Contains(err.Error(), "暂不支持") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCellRefParts(t *testing.T) {
	cases := []struct {
		ref      string
		col, row int
		ok       bool
	}{
		{"A1", 0, 0, true},
		{"B2", 1, 1, true},
		{"AA23", 26, 22, true},
		{"$A$1", 0, 0, true},
		{"XFD1048576", 16383, 1048575, true},
		{"", 0, 0, false},
		{"A", 0, 0, false},
		{"1", 0, 0, false},
	}
	for _, c := range cases {
		col, row, ok := CellRefParts(c.ref)
		if ok != c.ok || col != c.col || row != c.row {
			t.Errorf("CellRefParts(%q) = %d,%d,%v want %d,%d,%v", c.ref, col, row, ok, c.col, c.row, c.ok)
		}
	}
}

func TestFormulaNumericTolerance(t *testing.T) {
	// ROUND must use banker's-safe rounding for the common cases.
	got := evalNum(t, "ROUND(2.5, 0)", nil)
	if math.Abs(got-3) > 1e-9 {
		t.Errorf("ROUND(2.5) = %v, want 3", got)
	}
}

func TestFormulaSliceSyntax(t *testing.T) {
	ctx := NewEvalContext()
	ctx.CurRow = 3
	ctx.Slice = func(cell, spec, condition string, curRow int) (any, error) {
		return fmt.Sprintf("%s|%s|%s|%d", cell, spec, condition, curRow), nil
	}
	v := evalOK(t, "C4[!0;!0]A", ctx)
	if v != "C4|!0;!0||3" {
		t.Errorf("slice = %#v", v)
	}
	// The condition between the braces is passed along. The lexer drops
	// whitespace, so the text comes back without spaces.
	v = evalOK(t, "C4[!0]A{C4 > $C4}", ctx)
	if v != "C4|!0|C4>$C4|3" {
		t.Errorf("slice with condition = %#v", v)
	}
}

func TestFormulaSliceNotSupported(t *testing.T) {
	if _, err := EvalFormula("C4[!0]A", NewEvalContext()); err == nil {
		t.Error("expected a slice without a hook to fail")
	}
}

func TestFormulaHierarchicalReference(t *testing.T) {
	ctx := NewEvalContext()
	ctx.LevelOf = func(ref string) (any, bool) {
		if ref == "A3" {
			return float64(1), true
		}
		return nil, false
	}
	// "&A3" is a hierarchical reference: it yields the number of the
	// level the cell sits on, which is how a template detects the first
	// row of an expansion.
	if got := evalNum(t, "&A3", ctx); got != 1 {
		t.Errorf("&A3 = %v, want 1", got)
	}
	if v := evalOK(t, "IF(&A3 = 1, 0, 99)", ctx); v != float64(0) {
		t.Errorf("IF(&A3 = 1) = %#v, want 0", v)
	}
	// Without the hook the reference is an error.
	if _, err := EvalFormula("&A3", NewEvalContext()); err == nil {
		t.Error("expected a hierarchical reference without a hook to fail")
	}
}

func TestFormulaLogicOperators(t *testing.T) {
	if v := evalOK(t, "1 = 1 || 2 = 3", nil); v != true {
		t.Errorf("|| = %#v", v)
	}
	if v := evalOK(t, "1 = 1 && 2 = 2", nil); v != true {
		t.Errorf("&& = %#v", v)
	}
	if v := evalOK(t, "1 = 2 && 2 = 2", nil); v != false {
		t.Errorf("&& false = %#v", v)
	}
	// The operators short-circuit, so a failing right side is not an error.
	if v := evalOK(t, "1 = 0 && 1/0 = 1", nil); v != false {
		t.Errorf("&& short circuit = %#v", v)
	}
	if v := evalOK(t, "1 = 1 || 1/0 = 1", nil); v != true {
		t.Errorf("|| short circuit = %#v", v)
	}
	// They also work inside a function call.
	if v := evalOK(t, "IF(1 = 0 || LEN(\"ab\") = 0, \"x\", \"y\")", nil); v != "y" {
		t.Errorf("IF with || = %#v", v)
	}
}

func TestFormulaSQLIsBlockedByDefault(t *testing.T) {
	// The SQL function needs a hook; without one the policy blocks it.
	_, err := EvalFormula(`SQL("FRDemo", "SELECT 1", 1, 1)`, NewEvalContext())
	if err == nil {
		t.Fatal("expected SQL to be blocked by default")
	}
	var pe *PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected a PolicyError, got %v", err)
	}
	if !strings.Contains(pe.Switch, "RV_ALLOW_FORMULA_SQL") {
		t.Errorf("the error should name the switch: %v", err)
	}
}

func TestFormulaSQLWithHook(t *testing.T) {
	ctx := NewEvalContext()
	ctx.FormulaSQL = func(conn, query string, row, col int) (any, error) {
		return fmt.Sprintf("%s|%s|%d|%d", conn, query, row, col), nil
	}
	v := evalOK(t, `SQL("FRDemo", "SELECT 1", 8, 23)`, ctx)
	if v != "FRDemo|SELECT 1|8|23" {
		t.Errorf("SQL = %#v", v)
	}
	// The argument count is checked.
	if _, err := EvalFormula(`SQL("FRDemo", "SELECT 1")`, ctx); err == nil {
		t.Error("expected SQL with too few arguments to fail")
	}
}

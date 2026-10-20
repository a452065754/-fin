package fr

import (
	"fmt"
	"math"
	"math/rand"
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
		if len(vals) < 2 {
			return nil, fmt.Errorf("IF 需要至少两个参数")
		}
		var f func(any) any
		f = func(c any) any {
			if truthy(c) {
				if len(vals) > 1 {
					return scalar(vals[1])
				}
				return true
			}
			if len(vals) > 2 {
				return scalar(vals[2])
			}
			return false
		}
		return vectorMap(vals[0], func(v any) (any, error) { return f(v), nil })
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
		if len(vals) < 1 {
			return 0.0, nil
		}
		f, _ := toFloat(vals[0])
		return f, nil
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
	case "IFERROR":
		// IFERROR is handled at the call site because errors abort evaluation
		// early. Here we only unbox the value argument.
		if len(vals) < 1 {
			return "", nil
		}
		return vals[0], nil
	}
	return nil, fmt.Errorf("暂不支持的函数 %s", e.name)
}

func (ctx *EvalContext) now() time.Time {
	if ctx.Now.IsZero() {
		return time.Now()
	}
	return ctx.Now
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
			toks = append(toks, token{kind: tkString, text: b.String()})
		case c == '$':
			start := i
			i++
			for i < len(s) && (isLetter(s[i]) || s[i] == '_') {
				i++
			}
			if i < len(s) && s[i] == '$' {
				i++
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
			} else {
				for i < len(s) && (isLetter(s[i]) || s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
					i++
				}
			}
			word := s[start:i]
			if isCellRef(word) {
				toks = append(toks, token{kind: tkIdent, text: strings.ToUpper(word)})
			} else {
				toks = append(toks, token{kind: tkParam, text: strings.TrimPrefix(word, "$")})
			}
		case isLetter(c) || c == '_':
			start := i
			for i < len(s) && (isLetter(s[i]) || s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
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
		case c == ',':
			toks = append(toks, token{kind: tkComma, text: ","})
			i++
		case c == ':':
			toks = append(toks, token{kind: tkColon, text: ":"})
			i++
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

func (p *formulaParser) parsePrimary() (formulaExpr, error) {
	t := p.next()
	switch t.kind {
	case tkNumber:
		return litExpr{v: t.num}, nil
	case tkString:
		return litExpr{v: t.text}, nil
	case tkParam:
		return paramExpr{name: t.text}, nil
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

package fr

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// EmuPerPx is the practical scale used for report geometry. FineReport stores
// positions in EMU; dividing by 96dpi natural scale makes demo pages several
// thousand pixels wide, so we use 32dpi boxes plus slightly enlarged fonts.
const EmuPerPx = 28575.0

func EmuToPx(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return v / EmuPerPx
}

// FontSizePx converts FineReport's 1/10pt font sizes to readable screen pixels.
func FontSizePx(sizeTenths float64) float64 {
	if sizeTenths <= 0 {
		return 0
	}
	px := sizeTenths * 0.21333
	if px < 7 {
		px = 7
	}
	if px > 96 {
		px = 96
	}
	return math.Round(px*10) / 10
}

// FRColor converts a signed 32-bit ARGB integer to #rrggbb.
func FRColor(v int64) string {
	u := uint32(v)
	a := u >> 24
	if a == 0 && v != 0 {
		a = 255
	}
	if a == 0 && v == 0 {
		return ""
	}
	r := (u >> 16) & 0xff
	g := (u >> 8) & 0xff
	b := u & 0xff
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func colorOfNode(n *XNode) string {
	if n == nil {
		return ""
	}
	if n.Attr("color") != "" {
		if v, err := strconv.ParseInt(n.Attr("color"), 10, 64); err == nil {
			return FRColor(v)
		}
	}
	if fc := n.Find("FineColor"); fc != nil {
		if v, err := strconv.ParseInt(fc.Attr("color"), 10, 64); err == nil {
			return FRColor(v)
		}
	}
	return ""
}

func alignH(v int) string {
	switch v {
	case 0:
		return "center"
	case 2:
		return "left"
	case 4:
		return "right"
	case 9:
		return "justify"
	}
	return ""
}

func alignV(v int) string {
	switch v {
	case 0:
		return "middle"
	case 1:
		return "top"
	case 3:
		return "bottom"
	}
	return ""
}

var borderStyles = map[int]string{
	1:  "1px solid",
	2:  "2px solid",
	4:  "1px solid",
	5:  "3px solid",
	6:  "3px double",
	3:  "1px dashed",
	7:  "1px dotted",
	8:  "2px dashed",
	9:  "1px dashed",
	10: "2px dashed",
	11: "1px dotted",
	12: "2px dotted",
	14: "1px solid",
	15: "2px dotted",
	16: "4px solid",
	17: "1px solid",
}

func borderCSS(n *XNode) string {
	if n == nil {
		return ""
	}
	st, _ := strconv.Atoi(n.Attr("style"))
	if st == 0 {
		return ""
	}
	base, ok := borderStyles[st]
	if !ok {
		base = "1px solid"
	}
	col := colorOfNode(n)
	if col == "" {
		col = "#000000"
	}
	return base + " " + col
}

// ParseStyleNode reads a FineReport <Style> element.
func ParseStyleNode(n *XNode) Style {
	var s Style
	if n == nil {
		return s
	}
	if v, ok := n.AttrMap["horizontal_alignment"]; ok {
		i, _ := strconv.Atoi(v)
		s.HAlign = alignH(i)
	}
	if v, ok := n.AttrMap["vertical_alignment"]; ok {
		i, _ := strconv.Atoi(v)
		s.VAlign = alignV(i)
	}
	if v, ok := n.AttrMap["text_style"]; ok {
		i, _ := strconv.Atoi(v)
		s.Wrap = i == 0
	}
	if f := n.Child("FRFont"); f != nil {
		s.FontFamily = f.Attr("name")
		size, _ := strconv.ParseFloat(f.Attr("size"), 64)
		s.FontSize = FontSizePx(size)
		fs, _ := strconv.Atoi(f.Attr("style"))
		s.Bold = fs&1 != 0
		s.Italic = fs&2 != 0
		s.Underline = fs&4 != 0 || f.Attr("underline") == "1" || f.Attr("underline") == "true"
		if col := f.Attr("foreground"); col != "" {
			if v, err := strconv.ParseInt(col, 10, 64); err == nil {
				s.Color = FRColor(v)
			}
		}
		if c := f.Child("foreground"); c != nil && s.Color == "" {
			s.Color = colorOfNode(c)
		}
	}
	if bg := n.Child("Background"); bg != nil {
		name := bg.Attr("name")
		switch {
		case strings.Contains(name, "Color"):
			if c := bg.Child("color"); c != nil {
				s.Background = colorOfNode(c)
			}
		case strings.Contains(name, "Image"):
			s.Background = "" // image backgrounds are handled by FVS/background layers
		}
	}
	if b := n.Child("Border"); b != nil {
		s.BorderTop = borderCSS(b.Child("Top"))
		s.BorderRight = borderCSS(b.Child("Right"))
		s.BorderBottom = borderCSS(b.Child("Bottom"))
		s.BorderLeft = borderCSS(b.Child("Left"))
	}
	if f := n.Child("Format"); f != nil {
		s.NumberFormat = f.TextTrim()
	}
	return s
}

// ParseStyleList parses the workbook-level <StyleList>.
func ParseStyleList(workbook *XNode) []Style {
	var out []Style
	if workbook == nil {
		return out
	}
	sl := workbook.Child("StyleList")
	if sl == nil {
		sl = workbook.Find("StyleList")
	}
	if sl == nil {
		return out
	}
	for _, n := range sl.ChildAll("Style") {
		out = append(out, ParseStyleNode(n))
	}
	return out
}

// CellStyle resolves a cell's style by index, applying inline overrides.
func CellStyle(cell *XNode, styles []Style) Style {
	var s Style
	if cell != nil {
		idx, err := strconv.Atoi(cell.Attr("s"))
		if err == nil && idx >= 0 && idx < len(styles) {
			s = styles[idx]
		}
		if inline := cell.Child("Style"); inline != nil {
			s = mergeStyle(s, ParseStyleNode(inline))
		}
	}
	return s
}

func mergeStyle(base, over Style) Style {
	if over.FontFamily != "" {
		base.FontFamily = over.FontFamily
	}
	if over.FontSize > 0 {
		base.FontSize = over.FontSize
	}
	if over.Color != "" {
		base.Color = over.Color
	}
	if over.Background != "" {
		base.Background = over.Background
	}
	if over.HAlign != "" {
		base.HAlign = over.HAlign
	}
	if over.VAlign != "" {
		base.VAlign = over.VAlign
	}
	if over.BorderTop != "" {
		base.BorderTop = over.BorderTop
	}
	if over.BorderRight != "" {
		base.BorderRight = over.BorderRight
	}
	if over.BorderBottom != "" {
		base.BorderBottom = over.BorderBottom
	}
	if over.BorderLeft != "" {
		base.BorderLeft = over.BorderLeft
	}
	if over.NumberFormat != "" {
		base.NumberFormat = over.NumberFormat
	}
	base.Bold = base.Bold || over.Bold
	base.Italic = base.Italic || over.Italic
	base.Underline = base.Underline || over.Underline
	base.Wrap = base.Wrap || over.Wrap
	return base
}

// FormatValue applies an Excel-style number/date pattern to a value.
func FormatValue(v any, pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "General" || pattern == "general" {
		return plainValue(v)
	}
	if isDatePattern(pattern) {
		if t, ok := toTime(v); ok {
			return t.Format(javaDateLayout(pattern))
		}
	}
	f, ok := toFloat(v)
	if !ok {
		return plainValue(v)
	}
	return formatNumber(f, pattern)
}

func isDatePattern(p string) bool {
	lp := strings.ToLower(p)
	return strings.Contains(lp, "yyyy") || strings.Contains(lp, "yy年") ||
		(strings.Contains(lp, "yy") && (strings.Contains(lp, "mm") || strings.Contains(lp, "dd")))
}

func javaDateLayout(p string) string {
	r := strings.NewReplacer(
		"YYYY", "2006", "yyyy", "2006", "YY", "06", "yy", "06",
		"MM", "01", "M", "1",
		"DD", "02", "dd", "02", "d", "2",
		"HH", "15", "hh", "15", "mm", "04", "ss", "05",
	)
	out := r.Replace(p)
	// Protect quoted literals: FineReport date patterns use single quotes.
	out = strings.ReplaceAll(out, "'", "")
	return out
}

func formatNumber(f float64, pattern string) string {
	neg := f < 0
	af := math.Abs(f)
	pct := strings.Contains(pattern, "%")
	prefix, suffix := "", ""
	if pct {
		af *= 100
		suffix = "%"
	}
	// Extract literal prefix/suffix around the numeric placeholder.
	start := strings.IndexAny(pattern, "#0")
	end := strings.LastIndexAny(pattern, "#0")
	if start < 0 {
		return plainValue(f)
	}
	prefix = stripQuotes(pattern[:start])
	// With a percent marker the trailing "%" of the pattern IS the marker,
	// not a literal suffix; keeping both produced strings like "19.04%%".
	suffixLit := pattern[end+1:]
	if pct {
		suffixLit = strings.TrimSuffix(suffixLit, "%")
	}
	suffix = stripQuotes(suffixLit) + suffix
	numPart := pattern[start : end+1]
	if pct {
		numPart = strings.TrimSuffix(numPart, "%")
	}
	decimals := 0
	if i := strings.Index(numPart, "."); i >= 0 {
		decimals = len(strings.TrimRight(numPart[i+1:], "%"))
	}
	grouping := strings.Contains(numPart, ",")
	scale := 1.0
	if strings.Contains(numPart, "E") || strings.Contains(numPart, "e") {
		s := strconv.FormatFloat(af, 'E', decimals, 64)
		return prefix + s + suffix
	}
	for _, token := range []struct {
		word string
		v    float64
	}{{"千", 1000}, {"万", 10000}, {"百万", 1e6}} {
		if strings.Contains(prefix+suffix, token.word) {
			scale = token.v
			prefix = strings.ReplaceAll(prefix, token.word, "")
			suffix = strings.ReplaceAll(suffix, token.word, "")
			break
		}
	}
	af /= scale
	var out string
	if grouping {
		raw := strconv.FormatFloat(af, 'f', decimals, 64)
		parts := strings.SplitN(raw, ".", 2)
		intPart := parts[0]
		var b strings.Builder
		for i, ch := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				b.WriteByte(',')
			}
			b.WriteRune(ch)
		}
		if len(parts) > 1 {
			out = b.String() + "." + parts[1]
		} else {
			out = b.String()
		}
	} else {
		out = strconv.FormatFloat(af, 'f', decimals, 64)
	}
	if neg {
		out = "-" + out
	}
	return prefix + out + suffix
}

func stripQuotes(s string) string {
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}

func plainValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatFloat(t, 'f', -1, 64)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = plainValue(e)
		}
		return strings.Join(parts, " ")
	case []float64:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = plainValue(e)
		}
		return strings.Join(parts, " ")
	case []int:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = strconv.Itoa(e)
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(t)
	}
}

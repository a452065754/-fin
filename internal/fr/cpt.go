package fr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CPTTemplate is a parsed .cpt workbook.
type CPTTemplate struct {
	Path     string
	Name     string
	Styles   []Style
	Sheets   []*SheetTemplate
	Datasets map[string]*Dataset
	Params   []ParamDef
	Warnings []string
	// H5 holds the mobile layout parameters. It is nil for ordinary
	// desktop templates.
	H5 *H5Spec
}

type SheetTemplate struct {
	Name       string
	Cells      map[[2]int]*CellTemplate
	CellList   []*CellTemplate
	RowHeights []float64 // px; 0 means collapsed
	ColWidths  []float64
	DefRow     float64
	DefCol     float64
	MaxRow     int
	MaxCol     int
	PaperW     float64
	PaperH     float64
	MarginL    float64
	MarginR    float64
	MarginT    float64
	MarginB    float64
	// Paging holds the pagination mode read from ReportSettings: "paper"
	// (paginate by paper size), "content" or "none" (single continuous page).
	Paging string
}

type DictSpec struct {
	Conn  string
	Table string
	KI    int
	VI    int
}

type CellTemplate struct {
	R, C, RS, CS int
	Kind         string // text, ds, formula, chart, unsupported, blank
	Text         string
	Formula      string
	DSName       string
	Column       string
	ExpandDir    int // -1 none, 0 vertical, 1 horizontal
	ParentRef    string
	HasExpand    bool
	RGMode       int
	HasRG        bool
	// RGSummary reports whether the cell's grouper is a SummaryGrouper
	// (aggregate such as sum) rather than a plain value grouper.
	RGSummary bool
	// RGFunc keeps the summary function class (SumFunction, CountFunction,
	// AverageFunction, MaxFunction, MinFunction) of a SummaryGrouper.
	RGFunc string
	Dict         *DictSpec
	Style        Style
	ChartXML     *XNode
	// Widget describes the fill-in control attached to the cell. It is nil
	// for ordinary report cells.
	Widget *WidgetSpec
}

// WidgetSpec describes a FineReport form control (填报控件). The control's
// current value is the cell's own value; the widget only decides how the
// value is presented and whether the user may change it.
type WidgetSpec struct {
	// Class is the control class, e.g. "com.fr.form.ui.NumberEditor".
	Class string `json:"class,omitempty"`
	// Kind is the short control name used by the frontend, e.g. "number".
	Kind string `json:"kind,omitempty"`
	// Editable reports whether the control accepts user input.
	Editable bool `json:"editable"`
	// Options holds the choices of a combo box or check box.
	Options []string `json:"options,omitempty"`
	// Description is the control's tooltip text.
	Description string `json:"description,omitempty"`
	// Bookmark is the mobile bookmark name, used to link controls.
	Bookmark string `json:"bookmark,omitempty"`
	// ScanCode reports whether the text control supports barcode scanning.
	ScanCode bool `json:"scanCode,omitempty"`
}

func (t *CPTTemplate) DocTitle() string { return t.Name }

// ParseCPTFile reads and parses a .cpt template from disk.
func ParseCPTFile(path string) (*CPTTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return ParseCPTBytes(data, name, path)
}

func ParseCPTBytes(data []byte, name, path string) (*CPTTemplate, error) {
	root, err := ParseXML(data)
	if err != nil {
		return nil, fmt.Errorf("解析 CPT XML 失败: %w", err)
	}
	if root == nil || root.Name != "WorkBook" {
		return nil, fmt.Errorf("不是有效的 CPT 文件")
	}
	t := &CPTTemplate{
		Path:     path,
		Name:     name,
		Styles:   ParseStyleList(root),
		Datasets: ParseDatasets(root),
		H5:       ParseH5(root),
	}
	t.Params = parseTemplateParams(root)
	for _, rep := range root.ChildAll("Report") {
		cel := rep.Child("CellElementList")
		if cel == nil {
			continue
		}
		sh := &SheetTemplate{Name: rep.Attr("name"), Cells: map[[2]int]*CellTemplate{}}
		if sh.Name == "" {
			sh.Name = fmt.Sprintf("sheet%d", len(t.Sheets)+1)
		}
		sh.parseSizes(rep, root)
		for _, c := range cel.ChildAll("C") {
			ct := parseCell(c, t.Styles)
			if ct == nil {
				continue
			}
			key := [2]int{ct.R, ct.C}
			sh.Cells[key] = ct
			sh.CellList = append(sh.CellList, ct)
			if ct.R+ct.RS > sh.MaxRow {
				sh.MaxRow = ct.R + ct.RS
			}
			if ct.C+ct.CS > sh.MaxCol {
				sh.MaxCol = ct.C + ct.CS
			}
		}
		sh.resolveExpandParents()
		sh.fillSizes()
		sort.Slice(sh.CellList, func(i, j int) bool {
			if sh.CellList[i].R != sh.CellList[j].R {
				return sh.CellList[i].R < sh.CellList[j].R
			}
			return sh.CellList[i].C < sh.CellList[j].C
		})
		t.Sheets = append(t.Sheets, sh)
	}
	if len(t.Sheets) == 0 {
		return nil, fmt.Errorf("CPT 中没有可渲染的 Report 页")
	}
	return t, nil
}

func parseTemplateParams(root *XNode) []ParamDef {
	var out []ParamDef
	if rpa := root.Child("ReportParameterAttr"); rpa != nil {
		widgets := parseParamWidgets(rpa)
		for _, p := range rpa.ChildAll("Parameter") {
			name := p.Attr("name")
			if a := p.Child("Attributes"); a != nil {
				name = a.Attr("name")
			}
			if name == "" {
				continue
			}
			def := ""
			if o := p.Child("O"); o != nil {
				def = strings.TrimSpace(o.TextTrim())
			}
			pd := ParamDef{Name: name, Default: def}
			if w, ok := widgets[name]; ok {
				pd.Type = w.Type
				pd.Options = w.Options
				if w.Label != "" {
					pd.Label = w.Label
				}
			}
			if pd.Type == "" {
				pd.Type = "text"
			}
			out = append(out, pd)
		}
	}
	return out
}

// paramWidget captures the UI control bound to a template parameter so the
// viewer can render a real filter control (dropdown, date picker, …) instead
// of a bare text box.
type paramWidget struct {
	Type    string
	Label   string
	Options []ParamOption
}

// parseParamWidgets walks the parameter-panel layout and returns, keyed by
// parameter name, the control type and any dropdown options. FineReport
// stores the panel as a full form whose widgets carry both the parameter
// name (WidgetName) and the editor class (InnerWidget).
func parseParamWidgets(rpa *XNode) map[string]paramWidget {
	out := map[string]paramWidget{}
	ui := rpa.Child("ParameterUI")
	if ui == nil {
		return out
	}
	layout := ui.Child("Layout")
	if layout == nil {
		return out
	}
	for _, w := range layout.ChildAll("Widget") {
		iw := w.Child("InnerWidget")
		if iw == nil {
			continue
		}
		var name string
		if wn := iw.Child("WidgetName"); wn != nil {
			name = wn.Attr("name")
		}
		if name == "" {
			continue
		}
		cls := iw.Attr("class")
		pw := out[name]
		pw.Type = widgetTypeOf(cls)
		pw.Options = paramComboOptions(iw)
		// A label widget placed just before the control often carries the
		// human-readable caption; capture it when available.
		if lbl := paramWidgetLabel(w); lbl != "" {
			pw.Label = lbl
		}
		out[name] = pw
	}
	return out
}

// widgetTypeOf maps a FineReport editor class to the viewer control kind.
func widgetTypeOf(cls string) string {
	switch {
	case strings.Contains(cls, "ComboBox"), strings.Contains(cls, "ComboCheckBox"),
		strings.Contains(cls, "TreeComboBox"):
		return "combo"
	case strings.Contains(cls, "DateEditor"), strings.Contains(cls, "DateTimeEditor"):
		return "date"
	case strings.Contains(cls, "NumberEditor"):
		return "number"
	case strings.Contains(cls, "CheckBox"), strings.Contains(cls, "Radio"):
		return "checkbox"
	default:
		return "text"
	}
}

// paramComboOptions extracts the selectable values from a combo's dictionary.
// Only the static CustomDictionary form is decoded here; a TableDataDictionary
// would require running another dataset query, which the parser does not do.
func paramComboOptions(iw *XNode) []ParamOption {
	dict := iw.Child("Dictionary")
	if dict == nil {
		return nil
	}
	// The dictionary kind is carried by the class attribute; the option list
	// itself lives under <CustomDictAttr> for a custom dictionary.
	attr := dict.Child("CustomDictAttr")
	if attr == nil {
		return nil
	}
	var opts []ParamOption
	for _, d := range attr.ChildAll("Dict") {
		k := d.Attr("key")
		v := d.Attr("value")
		if k == "" && v == "" {
			continue
		}
		if v == "" {
			v = k
		}
		opts = append(opts, ParamOption{Value: k, Label: v})
	}
	return opts
}

func paramWidgetLabel(w *XNode) string {
	// The label is sometimes a sibling Label widget; we only read an inline
	// caption from the control's own WidgetTitle when present.
	if t := w.Child("WidgetTitle"); t != nil {
		if o := t.Child("O"); o != nil {
			return strings.TrimSpace(o.TextTrim())
		}
	}
	return ""
}

func (sh *SheetTemplate) parseSizes(rep, root *XNode) {
	sh.DefRow = EmuToPx(723900)
	sh.DefCol = EmuToPx(2743200)
	if rh := rep.Child("RowHeight"); rh != nil {
		def, _ := strconv.ParseFloat(rh.Attr("defaultValue"), 64)
		if def > 0 {
			sh.DefRow = EmuToPx(def)
		}
		sh.RowHeights = parseEmuList(rh.Text)
	}
	if cw := rep.Child("ColumnWidth"); cw != nil {
		def, _ := strconv.ParseFloat(cw.Attr("defaultValue"), 64)
		if def > 0 {
			sh.DefCol = EmuToPx(def)
		}
		sh.ColWidths = parseEmuList(cw.Text)
	}
	// WebPage isPage decides whether the web preview paginates by paper. When
	// isPage="false" the report shows as one continuous HTML page, so an empty
	// PaperSetting must stay unpaginated (e.g. GettingStarted.cpt). A missing
	// WebPage element means the FineReport default, which is to paginate.
	// ReportWebAttr lives at the WorkBook level (a sibling of Report) and the
	// WebPage element is nested inside its WebPageContent, so both are located
	// with a recursive search.
	isPage := true
	if rwa := root.Child("ReportWebAttr"); rwa != nil {
		if wp := rwa.Find("WebPage"); wp != nil {
			isPage = wp.AttrBool("isPage", true)
		}
	} else if rwa := rep.Child("ReportWebAttr"); rwa != nil {
		if wp := rwa.Find("WebPage"); wp != nil {
			isPage = wp.AttrBool("isPage", true)
		}
	}
	if attr := rep.Child("ReportAttrSet"); attr != nil {
		if rs := attr.Child("ReportSettings"); rs != nil {
			if ps := rs.Child("PaperSetting"); ps != nil {
				if paper := ps.Child("PaperSize"); paper != nil {
					sh.PaperW = EmuToPx(paper.AttrFloat("width", 0))
					sh.PaperH = EmuToPx(paper.AttrFloat("height", 0))
				}
				if m := ps.Child("Margin"); m != nil {
					sh.MarginL = EmuToPx(m.AttrFloat("left", 0))
					sh.MarginR = EmuToPx(m.AttrFloat("right", 0))
					sh.MarginT = EmuToPx(m.AttrFloat("top", 0))
					sh.MarginB = EmuToPx(m.AttrFloat("bottom", 0))
				}
			}
			// FineReport treats an empty PaperSetting as A4 for a paginated
			// preview. Default to A4 at 96dpi (794x1123px) so paper
			// pagination can apply; a template that opts out of web paging
			// (WebPage isPage="false") keeps no paper and renders as one
			// continuous page.
			if sh.PaperW <= 0 && isPage {
				sh.PaperW = 794
				sh.PaperH = 1123
			}
			sh.Paging = detectPagingMode(rs, sh.PaperW)
		}
	}
}

// detectPagingMode reads FineReport's paging setting. parseSizes has already
// substituted A4 (794x1123px) for an empty PaperSetting when the template
// paginates on the web, so a positive paperW here means "paginate by paper"
// unless the template explicitly turns vertical paging off.
func detectPagingMode(rs *XNode, paperW float64) string {
	if !rs.AttrBool("VerticallyPaging", true) {
		return "none"
	}
	if v := rs.Attr("WebPagingMode"); v == "0" || v == "3" {
		return "none"
	}
	if paperW <= 0 {
		return "none"
	}
	return "paper"
}

func parseEmuList(text string) []float64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			v = 0
		}
		out = append(out, EmuToPx(v))
	}
	return out
}

func (sh *SheetTemplate) fillSizes() {
	for len(sh.ColWidths) < sh.MaxCol {
		sh.ColWidths = append(sh.ColWidths, sh.DefCol)
	}
	for len(sh.RowHeights) < sh.MaxRow {
		sh.RowHeights = append(sh.RowHeights, sh.DefRow)
	}
	if sh.MaxCol == 0 {
		sh.MaxCol = 1
		sh.ColWidths = []float64{sh.DefCol}
	}
	if sh.MaxRow == 0 {
		sh.MaxRow = 1
		sh.RowHeights = []float64{sh.DefRow}
	}
}

// biasLabels splits a cross-table corner header ("销售员|地区") into its two
// diagonal labels. Returns nil for any other cell kind.
func biasLabels(ct *CellTemplate) []string {
	if ct == nil || ct.Kind != "biastext" {
		return nil
	}
	parts := strings.SplitN(ct.Text, "|", 2)
	out := make([]string, 0, 2)
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	if len(out) == 2 && (out[0] != "" || out[1] != "") {
		return out
	}
	return nil
}

func parseCell(c *XNode, styles []Style) *CellTemplate {
	ct := &CellTemplate{
		R:         c.AttrInt("r", 0),
		C:         c.AttrInt("c", 0),
		RS:        c.AttrInt("rs", 1),
		CS:        c.AttrInt("cs", 1),
		ExpandDir: -1,
		Kind:      "blank",
		Style:     CellStyle(c, styles),
		ParentRef: "",
	}
	if ct.RS < 1 {
		ct.RS = 1
	}
	if ct.CS < 1 {
		ct.CS = 1
	}
	o := c.Child("O")
	if o != nil {
		typ := o.Attr("t")
		switch typ {
		case "":
			ct.Kind = "text"
			ct.Text = o.Text
		case "String":
			ct.Kind = "text"
			ct.Text = o.Text
		case "DSColumn":
			ct.Kind = "ds"
			a := o.Child("Attributes")
			if a != nil {
				ct.DSName = a.Attr("dsName")
				ct.Column = a.Attr("columnName")
				if ct.Column == "" {
					ct.Column = strings.TrimSpace(a.TextTrim())
				}
			}
			if ct.DSName == "" {
				ct.DSName = o.Attr("dsName")
				ct.Column = o.Attr("columnName")
			}
		case "XMLable":
			if strings.Contains(o.Attr("class"), "Formula") {
				ct.Kind = "formula"
				if a := o.Child("Attributes"); a != nil {
					ct.Formula = strings.TrimSpace(a.TextTrim())
				} else {
					ct.Formula = strings.TrimSpace(o.TextTrim())
				}
			} else {
				ct.Kind = "unsupported"
				ct.Text = o.TextTrim()
			}
		case "Formula":
			ct.Kind = "formula"
			if a := o.Child("Attributes"); a != nil {
				ct.Formula = strings.TrimSpace(a.TextTrim())
			} else {
				ct.Formula = strings.TrimSpace(o.TextTrim())
			}
		case "BiasTextPainter":
			// Cross-table corner header: "销售员|地区" split by a diagonal.
			ct.Kind = "biastext"
			ct.Text = strings.TrimSpace(o.TextTrim())
		case "CC":
			ct.Kind = "chart"
			ct.ChartXML = o.Child("Chart")
			if ct.ChartXML == nil {
				ct.ChartXML = o.Find("Chart")
			}
		case "I", "D", "B", "BigDecimal", "BigInteger", "Date":
			ct.Kind = "text"
			ct.Text = strings.TrimSpace(o.TextTrim())
		case "Image":
			ct.Kind = "unsupported"
			ct.Text = ""
		case "RichText":
			ct.Kind = "text"
			if tn := o.Find("Text"); tn != nil {
				ct.Text = tn.TextTrim()
			} else {
				ct.Text = o.TextTrim()
			}
		default:
			ct.Kind = "unsupported"
			ct.Text = o.TextTrim()
		}
	}
	if exp := c.Child("Expand"); exp != nil {
		ct.HasExpand = true
		if _, ok := exp.AttrMap["dir"]; ok {
			ct.ExpandDir = exp.AttrInt("dir", -1)
		}
		if ct.ExpandDir < 0 {
			if up := exp.Attr("up"); up != "" {
				ct.ParentRef = up
			} else if left := exp.Attr("left"); left != "" {
				ct.ParentRef = left
			}
		}
	}
	rg := c.Child("RG")
	if rg == nil && o != nil {
		rg = o.Child("RG")
	}
	if rg != nil {
		ct.HasRG = true
		ct.RGMode = rg.AttrInt("divideMode", 0)
		if strings.Contains(rg.Attr("class"), "SummaryGrouper") {
			ct.RGSummary = true
			if fn := rg.Child("FN"); fn != nil {
				ct.RGFunc = fn.TextTrim()
			}
		}
	}
	if p := c.Child("Present"); p != nil {
		if d := p.Child("Dictionary"); d != nil {
			ct.Dict = &DictSpec{Conn: "FRDemo", KI: 0, VI: 1}
			if dd := d.Child("DBDictAttr"); dd != nil {
				ct.Dict.Table = dd.Attr("tableName")
				ct.Dict.KI = dd.AttrInt("ki", 0)
				ct.Dict.VI = dd.AttrInt("vi", 1)
			} else {
				ct.Dict.Table = d.Attr("tableName")
			}
			if conn := d.Find("Connection"); conn != nil {
				if n := conn.Find("DatabaseName"); n != nil {
					ct.Dict.Conn = n.TextTrim()
				}
			}
			if ct.Dict.Table == "" {
				ct.Dict = nil
			}
		}
	}
	// A fill-in control may sit next to the cell's value; the value itself
	// is still read from the <O> element above.
	ct.Widget = ParseWidget(c)
	return ct
}

func (sh *SheetTemplate) cellAtRef(ref string) *CellTemplate {
	col, row, ok := CellRefParts(ref)
	if !ok {
		return nil
	}
	return sh.Cells[[2]int{row, col}]
}

func (sh *SheetTemplate) resolveExpandParents() {
	var resolve func(ct *CellTemplate, seen map[*CellTemplate]bool) int
	resolve = func(ct *CellTemplate, seen map[*CellTemplate]bool) int {
		if ct.ExpandDir >= 0 || ct.ParentRef == "" || seen[ct] {
			return ct.ExpandDir
		}
		seen[ct] = true
		parent := sh.cellAtRef(ct.ParentRef)
		if parent == nil {
			return ct.ExpandDir
		}
		ct.ExpandDir = resolve(parent, seen)
		return ct.ExpandDir
	}
	for _, ct := range sh.CellList {
		resolve(ct, map[*CellTemplate]bool{})
	}
}

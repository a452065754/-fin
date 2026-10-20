package fr

import "strings"

// widgetKinds maps a FineReport form control class onto the short name the
// frontend uses. Controls that are not listed fall back to their simple
// class name so an unknown control still renders as a plain field.
var widgetKinds = map[string]string{
	"com.fr.form.ui.NumberEditor":        "number",
	"com.fr.form.ui.TextEditor":          "text",
	"com.fr.form.ui.TextAreaEditor":      "textarea",
	"com.fr.form.ui.ComboBox":            "combo",
	"com.fr.form.ui.TreeComboBoxEditor":  "tree",
	"com.fr.form.ui.DateEditor":          "date",
	"com.fr.form.ui.CheckBox":            "checkbox",
	"com.fr.form.ui.RadioBox":            "radio",
	"com.fr.form.ui.FreeButton":          "button",
	"com.fr.form.ui.DropDownList":        "dropdown",
	"com.fr.form.ui.MoneyEditor":         "money",
	"com.fr.form.ui.PercentEditor":       "percent",
	"com.fr.form.ui.MultiComboBox":       "multicombo",
	"com.fr.form.ui.ImageEditor":         "image",
	"com.fr.form.ui.FileEditor":          "file",
	"com.fr.form.ui.PasswordEditor":      "password",
	"com.fr.form.ui.CustomToolBarButton": "toolbar",
}

// readOnlyWidgets are controls that only trigger an action; they never carry
// a value the user can change.
var readOnlyWidgets = map[string]bool{
	"com.fr.form.ui.FreeButton":          true,
	"com.fr.form.ui.CustomToolBarButton": true,
	"com.fr.form.ui.ImageEditor":         true,
}

// ParseWidget reads the fill-in control attached to a cell. It returns nil
// when the cell carries no control.
func ParseWidget(c *XNode) *WidgetSpec {
	if c == nil {
		return nil
	}
	w := c.Child("Widget")
	if w == nil {
		return nil
	}
	class := w.Attr("class")
	if class == "" {
		return nil
	}
	spec := &WidgetSpec{Class: class, Kind: widgetKind(class), Editable: !readOnlyWidgets[class]}
	if attr := w.Child("WidgetAttr"); attr != nil {
		spec.Description = attr.Attr("description")
		if bm := attr.Child("MobileBookMark"); bm != nil {
			spec.Bookmark = bm.Attr("bookMarkName")
		}
	}
	// A combo box carries its choices in a custom dictionary.
	spec.Options = widgetOptions(w)
	// The text control may support barcode scanning on mobile.
	if scan := w.Child("MobileScanCodeAttr"); scan != nil {
		spec.ScanCode = scan.AttrBool("scanCode", false)
	}
	return spec
}

// widgetKind returns the short control name, falling back to the last part
// of the class name for controls the project does not know yet.
func widgetKind(class string) string {
	if k, ok := widgetKinds[class]; ok {
		return k
	}
	if i := strings.LastIndex(class, "."); i >= 0 && i+1 < len(class) {
		name := class[i+1:]
		// Drop a trailing "Editor" so "FooEditor" reads as "foo".
		name = strings.TrimSuffix(name, "Editor")
		return strings.ToLower(name)
	}
	return "text"
}

// widgetOptions extracts the selectable options of a combo box or check box.
func widgetOptions(w *XNode) []string {
	dict := w.Child("Dictionary")
	if dict == nil {
		return nil
	}
	attr := dict.Child("CustomDictAttr")
	if attr == nil {
		return nil
	}
	var out []string
	for _, d := range attr.ChildAll("Dict") {
		key := d.Attr("key")
		if key == "" {
			key = d.Attr("value")
		}
		if key != "" {
			out = append(out, key)
		}
	}
	return out
}

// IsH5Template reports whether the template was authored for the mobile
// (H5) client. Such templates carry an ElementCaseMobileAttr node.
func IsH5Template(t *CPTTemplate) bool {
	if t == nil {
		return false
	}
	return t.H5 != nil
}

// H5Spec holds the mobile layout parameters of an H5 template.
type H5Spec struct {
	// Horizontal and Vertical report whether the mobile canvas pans in that
	// direction.
	Horizontal bool `json:"horizontal"`
	Vertical   bool `json:"vertical"`
	// Zoom reports whether pinch-zoom is allowed.
	Zoom bool `json:"zoom"`
	// Refresh reports whether the template refreshes itself.
	Refresh bool `json:"refresh"`
	// UseHTML reports whether the mobile client renders it as HTML.
	UseHTML bool `json:"useHTML"`
	// CanvasSize reports whether the mobile canvas size is used.
	CanvasSize bool `json:"canvasSize"`
}

// ParseH5 reads the mobile layout attributes of a template. It returns nil
// for ordinary desktop templates.
func ParseH5(root *XNode) *H5Spec {
	if root == nil {
		return nil
	}
	node := root.Find("ElementCaseMobileAttr")
	if node == nil {
		return nil
	}
	p := node.Child("ElementCaseMobileAttrProvider")
	if p == nil {
		return nil
	}
	return &H5Spec{
		Horizontal: p.AttrBool("horizontal", false),
		Vertical:   p.AttrBool("vertical", false),
		Zoom:       p.AttrBool("zoom", false),
		Refresh:    p.AttrBool("refresh", false),
		UseHTML:    p.AttrBool("isUseHTML", false),
		CanvasSize: p.AttrBool("isMobileCanvasSize", false),
	}
}

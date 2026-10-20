package fr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireWebrootForWidgets skips when the FineReport installation is missing.
func requireWebrootForWidgets(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(webroot, "WEB-INF", "reportlets")); err != nil {
		t.Skipf("未找到 FineReport 模板目录（%s），跳过", webroot)
	}
}

func TestParseWidgetNumberEditor(t *testing.T) {
	xml := `<WorkBook><Report name="s1"><CellElementList>
		<C c="1" r="1"><O t="DSColumn"><Attributes dsName="ds1" columnName="Amt"/></O>
		<Widget class="com.fr.form.ui.NumberEditor">
			<WidgetAttr aspectRatioLocked="false" description="金额">
				<MobileBookMark useBookMark="true" bookMarkName="bm1"/>
			</WidgetAttr>
			<NumberAttr><widgetValue/></NumberAttr>
		</Widget>
		</C>
	</CellElementList></Report></WorkBook>`
	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	c := root.Find("CellElementList").Child("C")
	w := ParseWidget(c)
	if w == nil {
		t.Fatal("expected a widget spec")
	}
	if w.Kind != "number" {
		t.Errorf("kind = %q, want number", w.Kind)
	}
	if !w.Editable {
		t.Error("expected the number editor to be editable")
	}
	if w.Description != "金额" {
		t.Errorf("description = %q", w.Description)
	}
	if w.Bookmark != "bm1" {
		t.Errorf("bookmark = %q", w.Bookmark)
	}
}

func TestParseWidgetComboBoxOptions(t *testing.T) {
	xml := `<WorkBook><Report name="s1"><CellElementList>
		<C c="1" r="1"><O t="DSColumn"><Attributes dsName="ds1" columnName="Dept"/></O>
		<Widget class="com.fr.form.ui.ComboBox">
			<WidgetAttr/>
			<Dictionary class="com.fr.data.impl.CustomDictionary">
				<CustomDictAttr>
					<Dict key="联动部" value="联动部"/>
					<Dict key="宣传部" value="宣传部"/>
				</CustomDictAttr>
			</Dictionary>
			<widgetValue/>
		</Widget>
		</C>
	</CellElementList></Report></WorkBook>`
	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	c := root.Find("CellElementList").Child("C")
	w := ParseWidget(c)
	if w == nil {
		t.Fatal("expected a widget spec")
	}
	if w.Kind != "combo" {
		t.Errorf("kind = %q, want combo", w.Kind)
	}
	if len(w.Options) != 2 || w.Options[0] != "联动部" {
		t.Errorf("options = %v", w.Options)
	}
}

func TestParseWidgetButtonIsNotEditable(t *testing.T) {
	xml := `<WorkBook><Report name="s1"><CellElementList>
		<C c="1" r="1"><O><![CDATA[删除]]></O>
		<Widget class="com.fr.form.ui.FreeButton"><WidgetAttr/></Widget>
		</C>
	</CellElementList></Report></WorkBook>`
	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	c := root.Find("CellElementList").Child("C")
	w := ParseWidget(c)
	if w == nil {
		t.Fatal("expected a widget spec")
	}
	if w.Kind != "button" {
		t.Errorf("kind = %q, want button", w.Kind)
	}
	if w.Editable {
		t.Error("a button must not be editable")
	}
}

func TestParseWidgetAbsent(t *testing.T) {
	xml := `<WorkBook><Report name="s1"><CellElementList>
		<C c="1" r="1"><O><![CDATA[普通单元格]]></O></C>
	</CellElementList></Report></WorkBook>`
	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	c := root.Find("CellElementList").Child("C")
	if w := ParseWidget(c); w != nil {
		t.Errorf("expected no widget, got %+v", w)
	}
}

func TestParseH5Detection(t *testing.T) {
	desktop := `<WorkBook><Report name="s1"><CellElementList/></Report></WorkBook>`
	root, err := ParseXML([]byte(desktop))
	if err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	if h5 := ParseH5(root); h5 != nil {
		t.Errorf("expected no H5 spec, got %+v", h5)
	}

	mobile := `<WorkBook>
		<ElementCaseMobileAttr>
			<ElementCaseMobileAttrProvider horizontal="1" vertical="0" zoom="true" refresh="false" isUseHTML="true" isMobileCanvasSize="true"/>
		</ElementCaseMobileAttr>
		<Report name="s1"><CellElementList/></Report>
	</WorkBook>`
	root, err = ParseXML([]byte(mobile))
	if err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	h5 := ParseH5(root)
	if h5 == nil {
		t.Fatal("expected an H5 spec")
	}
	if !h5.Horizontal || h5.Vertical {
		t.Errorf("pan flags = %v/%v", h5.Horizontal, h5.Vertical)
	}
	if !h5.Zoom {
		t.Error("expected zoom to be enabled")
	}
	if !h5.UseHTML || !h5.CanvasSize {
		t.Errorf("useHTML=%v canvasSize=%v", h5.UseHTML, h5.CanvasSize)
	}
}

func TestH5TemplateRendersWithMarker(t *testing.T) {
	requireWebrootForWidgets(t)
	rel := `/WEB-INF/reportlets/demo/Phone/analytics/financial/公司回款额-phone.cpt`
	path := webroot + rel
	if _, err := os.Stat(path); err != nil {
		t.Skipf("模板不存在: %s", path)
	}
	tpl, err := ParseCPTFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tpl.H5 == nil {
		t.Fatal("expected the template to be marked as H5")
	}
	if !IsH5Template(tpl) {
		t.Error("IsH5Template should report true")
	}
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)
	if p.Error != "" {
		t.Fatalf("render: %s", p.Error)
	}
	if p.H5 == nil {
		t.Error("expected the preview to carry the H5 spec")
	}
	// The H5 marker must not break the normal cell rendering.
	cells := 0
	for _, sh := range p.Sheets {
		cells += len(sh.Cells)
	}
	if cells == 0 {
		t.Error("expected the H5 template to render cells")
	}
	t.Logf("H5 模板渲染 %d 个单元格, warnings=%d", cells, len(p.Warnings))
}

func TestFormTemplateRendersWidgets(t *testing.T) {
	requireWebrootForWidgets(t)
	rel := `/WEB-INF/reportlets/demo/form/固定资产(增删改).cpt`
	path := webroot + rel
	if _, err := os.Stat(path); err != nil {
		t.Skipf("模板不存在: %s", path)
	}
	tpl, err := ParseCPTFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)
	if p.Error != "" {
		t.Fatalf("render: %s", p.Error)
	}
	if !p.HasForm {
		t.Error("expected the preview to report a form")
	}
	widgets := 0
	valued := 0
	kinds := map[string]int{}
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Widget == nil {
				continue
			}
			widgets++
			kinds[c.Widget.Kind]++
			if c.Text != "" {
				valued++
			}
			// The value must not carry the report's own line break prefix.
			if strings.HasPrefix(c.Text, "\n") {
				t.Errorf("widget value kept its newline prefix: %q", c.Text)
			}
		}
	}
	if widgets == 0 {
		t.Fatal("expected the form template to render widgets")
	}
	// The template's dataset holds three rows, so the controls must show a
	// value rather than stay blank.
	if valued == 0 {
		t.Errorf("expected %d widgets, none carried a value", widgets)
	}
	t.Logf("控件 %d 个(有值 %d), 类型=%v", widgets, valued, kinds)
}

func TestWidgetOptionsReachThePreview(t *testing.T) {
	requireWebrootForWidgets(t)
	// Find a template that has a combo box with options.
	rel := `/WEB-INF/reportlets/demo/form/固定资产(增删改).cpt`
	path := webroot + rel
	if _, err := os.Stat(path); err != nil {
		t.Skipf("模板不存在: %s", path)
	}
	tpl, err := ParseCPTFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dr := NewDataResolver(webroot)
	defer dr.Close()
	p := tpl.Render(dr, nil)
	found := 0
	for _, sh := range p.Sheets {
		for _, c := range sh.Cells {
			if c.Widget == nil || c.Widget.Kind != "combo" {
				continue
			}
			if len(c.Widget.Options) == 0 {
				continue
			}
			found++
			t.Logf("combo r%d c%d options=%v value=%q", c.R, c.C, c.Widget.Options, c.Text)
		}
	}
	if found == 0 {
		t.Error("expected at least one combo box with options")
	}
}

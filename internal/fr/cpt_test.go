package fr

import (
	"testing"
)

// TestParseTemplateParamComboBox verifies that a parameter declared only
// through a parameter-panel widget (the template's <Parameters/> list is
// empty) is surfaced as a real combo parameter, and that its database
// dictionary is captured so the renderer can resolve the options at runtime.
//
// This mirrors GettingStarted.cpt's 地区 control, which FineReport renders as
// a ComboBox (com.fr.form.ui.ComboBox) whose options come from a
// DatabaseDictionary over the 销量 table on the FRDemo connection.
func TestParseTemplateParamComboBox(t *testing.T) {
	xml := `<WorkBook>
  <ReportParameterAttr>
    <Parameters/>
    <ParameterUI>
      <Layout>
        <Widget>
          <InnerWidget class="com.fr.form.ui.ComboBox">
            <WidgetName name="地区"/>
            <widgetValue><O>华东</O></widgetValue>
            <Dictionary class="com.fr.data.impl.DatabaseDictionary">
              <DBDictAttr tableName="销量" ki="0" vi="0"/>
              <Connection>
                <DatabaseName>FRDemo</DatabaseName>
              </Connection>
            </Dictionary>
          </InnerWidget>
        </Widget>
      </Layout>
    </ParameterUI>
  </ReportParameterAttr>
</WorkBook>`

	root, err := ParseXML([]byte(xml))
	if err != nil {
		t.Fatalf("解析 XML 失败: %v", err)
	}

	params := parseTemplateParams(root)
	if len(params) == 0 {
		t.Fatal("未解析出任何参数")
	}

	var region *ParamDef
	for i := range params {
		if params[i].Name == "地区" {
			region = &params[i]
			break
		}
	}
	if region == nil {
		t.Fatalf("未找到 地区 参数，已解析: %v", params)
	}

	if region.Type != "combo" {
		t.Errorf("地区 类型应为 combo，实际 %q", region.Type)
	}
	if region.Default != "华东" {
		t.Errorf("地区 默认值应为 华东，实际 %q", region.Default)
	}
	if region.Dict == nil {
		t.Fatal("地区 应携带 DatabaseDictionary 规格")
	}
	if region.Dict.Table != "销量" {
		t.Errorf("字典表应为 销量，实际 %q", region.Dict.Table)
	}
	if region.Dict.Conn != "FRDemo" {
		t.Errorf("字典连接应为 FRDemo，实际 %q", region.Dict.Conn)
	}
}

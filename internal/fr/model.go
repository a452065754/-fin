package fr

// TemplateInfo describes a reportlet file found on disk.
type TemplateInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Dir  string `json:"dir"`
	Size int64  `json:"size"`
}

type ParamDef struct {
	Name    string `json:"name"`
	Default string `json:"default"`
}

// Preview is the render result sent to the browser.
type Preview struct {
	Path     string     `json:"path"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Title    string     `json:"title"`
	Params   []ParamDef `json:"params"`
	Sheets   []Sheet    `json:"sheets,omitempty"`
	Pages    []FVSPage  `json:"pages,omitempty"`
	Width    float64    `json:"width,omitempty"`
	Height   float64    `json:"height,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
	Error    string     `json:"error,omitempty"`
	// H5 holds the mobile layout parameters of an H5 template. It is nil
	// for ordinary desktop templates.
	H5 *H5Spec `json:"h5,omitempty"`
	// HasForm reports whether the template carries fill-in controls.
	HasForm bool `json:"hasForm,omitempty"`
}

type Sheet struct {
	Name   string    `json:"name"`
	Width  float64   `json:"width"`
	Height float64   `json:"height"`
	Cols   []float64 `json:"cols"`
	Rows   []float64 `json:"rows"`
	Cells  []OutCell `json:"cells"`
	// Paper carries the report's paper setup (read from PaperSetting) so the
	// viewer can render on real paper and paginate.
	Paper *PaperInfo `json:"paper,omitempty"`
	// Pages holds the printed pages when the report paginates by paper size.
	// It is empty when the sheet is shown as a single continuous page.
	Pages []CPTPage `json:"pages,omitempty"`
	// RepeatRows lists output row indices that are page headers and repeat on
	// every printed page. Internal; not serialised to the browser.
	RepeatRows []int `json:"-"`
	// RepeatCols lists output column indices (left-fixed columns) that repeat
	// on every printed page when the sheet paginates horizontally. It is empty
	// for templates that declare no repeat columns. Internal; not serialised.
	RepeatCols []int `json:"-"`
}

// PaperInfo describes the report paper used for pagination. All sizes are in
// pixels at the 96dpi render scale (FineReport's EMU unit ÷ 28575).
type PaperInfo struct {
	Width   float64 `json:"width"`  // paper width in px
	Height  float64 `json:"height"` // paper height in px
	MarginL float64 `json:"marginL"`
	MarginR float64 `json:"marginR"`
	MarginT float64 `json:"marginT"`
	MarginB float64 `json:"marginB"`
	Mode    string  `json:"mode"` // "paper" | "content" | "none"
}

// CPTPage is one printed page of a paginated .cpt sheet. It is a self-
// contained mini-sheet with remapped row/column indices.
type CPTPage struct {
	PageNo int       `json:"pageNo"`
	Cols   []float64 `json:"cols"`
	Rows   []float64 `json:"rows"`
	Cells  []OutCell `json:"cells"`
	Width  float64   `json:"width"`
	Height float64   `json:"height"`
}

type OutCell struct {
	R     int        `json:"r"`
	C     int        `json:"c"`
	RS    int        `json:"rs"`
	CS    int        `json:"cs"`
	Text  string     `json:"text,omitempty"`
	// Bias carries the two labels of a cross-table corner header
	// (BiasTextPainter, e.g. "销售员|地区") rendered split by a diagonal.
	Bias   []string   `json:"bias,omitempty"`
	Style  Style      `json:"style,omitempty"`
	Chart  *ChartSpec `json:"chart,omitempty"`
	// Widget is set when the cell carries a fill-in control.
	Widget *WidgetSpec `json:"widget,omitempty"`
}

type Style struct {
	FontFamily   string  `json:"fontFamily,omitempty"`
	FontSize     float64 `json:"fontSize,omitempty"`
	Bold         bool    `json:"bold,omitempty"`
	Italic       bool    `json:"italic,omitempty"`
	Underline    bool    `json:"underline,omitempty"`
	Color        string  `json:"color,omitempty"`
	Background   string  `json:"background,omitempty"`
	// BackgroundImage carries a decoded embedded background (a data URI) for
	// cells whose style uses an ImageBackground; BackgroundSize is its CSS
	// background-size.
	BackgroundImage string `json:"backgroundImage,omitempty"`
	BackgroundSize  string `json:"backgroundSize,omitempty"`
	HAlign       string  `json:"hAlign,omitempty"`
	VAlign       string  `json:"vAlign,omitempty"`
	BorderTop    string  `json:"borderTop,omitempty"`
	BorderRight  string  `json:"borderRight,omitempty"`
	BorderBottom string  `json:"borderBottom,omitempty"`
	BorderLeft   string  `json:"borderLeft,omitempty"`
	Wrap         bool    `json:"wrap,omitempty"`
	NumberFormat string  `json:"numberFormat,omitempty"`
}

type ChartSpec struct {
	Kind       string        `json:"kind"`
	Title      string        `json:"title,omitempty"`
	TitleColor string        `json:"titleColor,omitempty"`
	TitleSize  float64       `json:"titleSize,omitempty"`
	Categories []string      `json:"categories,omitempty"`
	Series     []ChartSeries `json:"series,omitempty"`
	Colors     []string      `json:"colors,omitempty"`
	Legend     bool          `json:"legend"`
	LegendPos  string        `json:"legendPos,omitempty"`
	Stack      bool          `json:"stack,omitempty"`
	Horizontal bool          `json:"horizontal,omitempty"`
	Warning    string        `json:"warning,omitempty"`

	// FineReport charts that map onto no single ECharts type. The browser
	// decides how faithfully it can draw them.
	// Combo charts mix several plot kinds; Series[].Type carries the per
	// series kind and is empty for ordinary charts.
	Combo       bool      `json:"combo,omitempty"`
	SizeRange   []float64 `json:"sizeRange,omitempty"`
	MapArea     string    `json:"mapArea,omitempty"`
	DrillDown   bool      `json:"drillDown,omitempty"`
	ExtraAxis   int       `json:"extraAxis,omitempty"`
	Horizontal2 bool      `json:"horizontal2,omitempty"`
	AxisLabel   []string  `json:"axisLabel,omitempty"`
	AxisValues  []float64 `json:"axisValues,omitempty"`

	// Gauge scale. GaugeMin/GaugeMax come from the plot's MC_Attr; when
	// GaugeAuto is set the browser may widen the scale to fit the data.
	// No omitempty on the bounds: a zero minimum is meaningful and the
	// browser needs to tell "min is 0" from "min is unknown".
	GaugeMin   float64     `json:"gaugeMin"`
	GaugeMax   float64     `json:"gaugeMax"`
	GaugeAuto  bool        `json:"gaugeAuto,omitempty"`
	GaugeBands []GaugeBand `json:"gaugeBands,omitempty"`

	// NeedsCellSource marks a chart whose data comes from report cells
	// rather than a dataset. The renderer fills it in a second pass.
	NeedsCellSource bool `json:"-"`

	// BoxSummary marks a box plot, whose series carry five-number summaries
	// instead of plain values.
	BoxSummary bool `json:"-"`

	// cellSource keeps the parsed cell references for that second pass.
	cellSource *chartCellSource
}

// GaugeBand is one coloured threshold band of a gauge dial.
type GaugeBand struct {
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Color string  `json:"color,omitempty"`
}

type ChartSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
	Type string    `json:"type,omitempty"`
	// Bubble charts need a third dimension: the point radius.
	Size []float64 `json:"size,omitempty"`
	// Dual-axis charts (combo line + bar) render against a second Y axis.
	Axis string `json:"axis,omitempty"`
	// BoxValues holds the five-number summary [min, q1, median, q3, max] of
	// each box, one entry per category.
	BoxValues [][]float64 `json:"boxValues,omitempty"`
}

type FVSPage struct {
	Name            string   `json:"name"`
	Width           float64  `json:"width,omitempty"`
	Height          float64  `json:"height,omitempty"`
	Background      string   `json:"background,omitempty"`
	BackgroundImage string   `json:"backgroundImage,omitempty"`
	BackgroundSize  string   `json:"backgroundSize,omitempty"`
	// Theme is "dark" when the page uses the FineReport dark-blue palette so
	// the frontend paints themed chart colors and axis labels.
	Theme   string   `json:"theme,omitempty"`
	Widgets []Widget `json:"widgets"`
}

type Widget struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Parent   string     `json:"parent,omitempty"`
	Pane     string     `json:"pane,omitempty"`
	X        float64    `json:"x"`
	Y        float64    `json:"y"`
	W        float64    `json:"w"`
	H        float64    `json:"h"`
	Z        int        `json:"z,omitempty"`
	Text     string     `json:"text,omitempty"`
	HTML     string     `json:"html,omitempty"`
	Style    TextStyle  `json:"style,omitempty"`
	ImageURL string     `json:"imageUrl,omitempty"`
	Chart    *ChartSpec `json:"chart,omitempty"`
	Sheet    *Sheet     `json:"sheet,omitempty"`
	Tab      *TabInfo   `json:"tab,omitempty"`
	// ClickParam names the dashboard parameter that a chart click feeds
	// (FineVis chart linkage, e.g. "area"). The browser listens for point
	// events and refetches the preview with the clicked category value.
	ClickParam string      `json:"clickParam,omitempty"`
	Link       *WidgetLink `json:"link,omitempty"`
	Hidden     bool        `json:"hidden,omitempty"`
}

// WidgetLink is a FineVis hyper-link action (e.g. the "点击查看FVS详情"
// text widget that opens a web page).
type WidgetLink struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type TextStyle struct {
	FontFamily    string  `json:"fontFamily,omitempty"`
	FontSize      float64 `json:"fontSize,omitempty"`
	FontWeight    string  `json:"fontWeight,omitempty"`
	Color         string  `json:"color,omitempty"`
	Align         string  `json:"align,omitempty"`
	VAlign        string  `json:"vAlign,omitempty"`
	LineHeight    string  `json:"lineHeight,omitempty"`
	LetterSpacing float64 `json:"letterSpacing,omitempty"`
	WritingMode   string  `json:"writingMode,omitempty"`
}

// TabInfo describes a slideshow/tab container.
type TabInfo struct {
	Tabs       []string `json:"tabs"`
	Active     int      `json:"active"`
	PaneIDs    []string `json:"paneIds"`
	Position   string   `json:"position,omitempty"`
	FontSize   float64  `json:"fontSize,omitempty"`
	FontColor  string   `json:"fontColor,omitempty"`
	ActiveBG   string   `json:"activeBg,omitempty"`
	InactiveBG string   `json:"inactiveBg,omitempty"`
}

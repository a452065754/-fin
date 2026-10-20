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
}

type Sheet struct {
	Name   string    `json:"name"`
	Width  float64   `json:"width"`
	Height float64   `json:"height"`
	Cols   []float64 `json:"cols"`
	Rows   []float64 `json:"rows"`
	Cells  []OutCell `json:"cells"`
}

type OutCell struct {
	R     int        `json:"r"`
	C     int        `json:"c"`
	RS    int        `json:"rs"`
	CS    int        `json:"cs"`
	Text  string     `json:"text,omitempty"`
	Style Style      `json:"style,omitempty"`
	Chart *ChartSpec `json:"chart,omitempty"`
}

type Style struct {
	FontFamily   string  `json:"fontFamily,omitempty"`
	FontSize     float64 `json:"fontSize,omitempty"`
	Bold         bool    `json:"bold,omitempty"`
	Italic       bool    `json:"italic,omitempty"`
	Underline    bool    `json:"underline,omitempty"`
	Color        string  `json:"color,omitempty"`
	Background   string  `json:"background,omitempty"`
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
}

type FVSPage struct {
	Name            string   `json:"name"`
	Width           float64  `json:"width,omitempty"`
	Height          float64  `json:"height,omitempty"`
	Background      string   `json:"background,omitempty"`
	BackgroundImage string   `json:"backgroundImage,omitempty"`
	BackgroundSize  string   `json:"backgroundSize,omitempty"`
	Widgets         []Widget `json:"widgets"`
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
	Hidden   bool       `json:"hidden,omitempty"`
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

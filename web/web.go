package webui

import "embed"

//go:embed index.html app.js style.css vendor/echarts.js
var FS embed.FS

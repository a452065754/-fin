package webui

import "embed"

//go:embed index.html app.js style.css vendor/echarts.js vendor/china.json
var FS embed.FS

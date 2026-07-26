// Package web 提供编译进可执行文件的前端静态资源。
package web

import "embed"

// Assets 包含 Web 界面的 HTML、CSS 和 JavaScript。
//
//go:embed index.html app.css app.js
var Assets embed.FS

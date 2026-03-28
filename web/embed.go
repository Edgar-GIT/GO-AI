package webui

import "embed"

//go:embed index.html styles.css app.js ai.ico
var Assets embed.FS

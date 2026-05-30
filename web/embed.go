// Package web は PWA の静的アセットを admin バイナリへ埋め込む。
package web

import "embed"

//go:embed static
var FS embed.FS

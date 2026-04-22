// Package web embeds the compiled React frontend.
package web

import "embed"

//go:embed dist
var FS embed.FS

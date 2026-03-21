package web

import "embed"

// StaticFS embeds the frontend static files.
// The frontend/ directory is copied into web/static/ during Docker build.
//
//go:embed static
var StaticFS embed.FS

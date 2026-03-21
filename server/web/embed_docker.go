//go:build docker

package web

import "embed"

//go:embed static
var embeddedStatic embed.FS

func init() {
	StaticFS = embeddedStatic
}

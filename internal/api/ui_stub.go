//go:build !embedui

package api

import (
	"embed"
	"io/fs"
)

//go:embed all:devui
var embeddedStubUI embed.FS

func embeddedStaticAssetsFS() (fs.FS, error) {
	return fs.Sub(embeddedStubUI, "devui")
}

//go:build embedui

package api

import (
	"embed"
	"io/fs"
)

//go:embed all:webui/dist
var embeddedWebDist embed.FS

func embeddedStaticAssetsFS() (fs.FS, error) {
	return fs.Sub(embeddedWebDist, "webui/dist")
}

//go:build !embedbins

package serverinstall

import (
	"embed"
	"io/fs"
)

//go:embed devdash/index.html
var devdash embed.FS

func dashboardRootFS() (fs.FS, error) {
	return fs.Sub(devdash, "devdash")
}

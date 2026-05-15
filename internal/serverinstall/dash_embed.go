//go:build embedbins

package serverinstall

import (
	"embed"
	"io/fs"
)

//go:embed all:dashboard
var prodDashboard embed.FS

func dashboardRootFS() (fs.FS, error) {
	return fs.Sub(prodDashboard, "dashboard")
}

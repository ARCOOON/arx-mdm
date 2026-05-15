//go:build linux

package packagemanager

import (
	"bufio"
	"os"
	"strings"
)

func listInstalled() ([]InstalledApp, error) {
	apps, err := listInstalledDPKG()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(apps) == 0 {
		return nil, nil
	}
	return trimCap(apps), nil
}

func listInstalledDPKG() ([]InstalledApp, error) {
	f, err := os.Open("/var/lib/dpkg/status")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []InstalledApp
	var curName, curVersion string
	var curInstalled bool

	flush := func() {
		if curInstalled && curName != "" {
			out = append(out, InstalledApp{
				Name:    curName,
				Version: curVersion,
				Source:  "dpkg",
				ID:      curName,
			})
		}
		curName, curVersion = "", ""
		curInstalled = false
	}

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "Package: ") {
			flush()
			curName = strings.TrimSpace(strings.TrimPrefix(line, "Package: "))
			continue
		}
		if strings.HasPrefix(line, "Version: ") {
			curVersion = strings.TrimSpace(strings.TrimPrefix(line, "Version: "))
			continue
		}
		if strings.HasPrefix(line, "Status: ") {
			st := strings.TrimSpace(strings.TrimPrefix(line, "Status: "))
			curInstalled = st == "install ok installed"
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func trimCap(in []InstalledApp) []InstalledApp {
	if len(in) <= MaxReportedInventory {
		return in
	}
	return in[:MaxReportedInventory]
}

package serverinstall

import (
	_ "embed"
	"net/http"
	"strconv"
)

//go:embed install_linux.sh
var linuxInstallScript []byte

//go:embed install_windows.ps1
var windowsInstallScript []byte

// Register adds public install script/binary download routes.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/install/linux", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(linuxInstallScript)
	})
	mux.HandleFunc("GET /v1/install/windows", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(windowsInstallScript)
	})

	mux.HandleFunc("GET /v1/install/bin/linux", func(w http.ResponseWriter, r *http.Request) {
		b := agentLinuxBytes()
		if len(b) == 0 {
			http.Error(w, "agent linux binary not available in this build (use Docker image or make build-all with embedbins)", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="arx-agent"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /v1/install/bin/windows", func(w http.ResponseWriter, r *http.Request) {
		b := agentWindowsBytes()
		if len(b) == 0 {
			http.Error(w, "agent windows binary not available in this build (use Docker image or make build-all with embedbins)", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="arx-agent.exe"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})

}

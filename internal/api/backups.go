package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/backup"
)

// BackupsDeps registers admin routes for inspecting and manipulating backup archives.
type BackupsDeps struct {
	Engine *backup.Engine
	Logger *slog.Logger
	Auth   DashboardAuth
}

type backupHandler struct {
	deps BackupsDeps
}

// RegisterBackupRoutes wires list, immediate trigger, and download endpoints (admin JWT only within handlers).
func RegisterBackupRoutes(mux *http.ServeMux, d BackupsDeps) {
	if mux == nil {
		panic("api: mux is nil")
	}
	if d.Engine == nil || d.Auth.JWT == nil {
		panic("api: backups routes require Engine and Auth.JWT")
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := backupHandler{
		deps: BackupsDeps{Engine: d.Engine, Logger: logger, Auth: d.Auth},
	}
	mux.HandleFunc("GET /v1/backups", h.handleList)
	mux.HandleFunc("POST /v1/backups/trigger", h.handleTrigger)
	mux.HandleFunc("GET /v1/backups/{filename}/download", h.handleDownload)
}

func (h *backupHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}
	items, err := h.deps.Engine.List()
	if err != nil {
		h.deps.Logger.Error("list backups failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "could not enumerate backups")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *backupHandler) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}
	timeout := 30 * time.Minute
	tctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	name, err := h.deps.Engine.RunOnce(tctx)
	if err != nil {
		h.deps.Logger.Error("manual backup trigger failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "backup failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"filename": name})
}

func (h *backupHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}
	raw := strings.TrimSpace(r.PathValue("filename"))
	if raw == "" {
		writeTicketsError(w, http.StatusBadRequest, "missing backup filename")
		return
	}
	if filepath.Base(raw) != raw {
		writeTicketsError(w, http.StatusBadRequest, "invalid backup filename")
		return
	}
	fullAbs, err := h.deps.Engine.ResolveSafePath(raw)
	if err != nil {
		writeTicketsError(w, http.StatusNotFound, "backup not available")
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, raw))
	http.ServeFile(w, r, fullAbs)
}

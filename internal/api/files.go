package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"arx-mdm/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxFileProxyChunk     = 256 * 1024
	fileProxyReadBodyBuf  = 256 * 1024
	fileProxyHTTPTimeout  = 20 * time.Minute
	maxUploadBytesPerCall = 512 << 20 // 512 MiB per upload request
)

// FilesDeps wires dashboard-authenticated asset file proxy routes.
type FilesDeps struct {
	Pool                         *pgxpool.Pool
	Logger                       *slog.Logger
	Auth                         DashboardAuth
	DispatchJSON                 func(certSerial string, payload any) bool
	RegisterFSDownloadWaiter     func(certSerial, requestID string) (<-chan C2FileChunk, func())
	RegisterFSUploadResultWaiter func(certSerial, requestID string) (<-chan C2FileUploadResult, func())
}

type filesHandler struct {
	deps FilesDeps
}

// NewFilesHandler registers /v1/assets/{id}/files/upload and /download (dashboard auth).
func NewFilesHandler(mux *http.ServeMux, d FilesDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: files handler requires Pool, Logger, and Auth.JWT")
	}
	if d.DispatchJSON == nil || d.RegisterFSDownloadWaiter == nil || d.RegisterFSUploadResultWaiter == nil {
		panic("api: files handler requires C2 dispatch/register callbacks")
	}
	h := &filesHandler{deps: d}
	mux.HandleFunc("GET /v1/assets/{id}/files/download", h.handleDownload)
	mux.HandleFunc("POST /v1/assets/{id}/files/upload", h.handleUpload)
}

func (h *filesHandler) authorizeOperator(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	return ok
}

func (h *filesHandler) resolveAsset(ctx context.Context, assetID string) (certSerial string, err error) {
	id, parseErr := uuid.Parse(strings.TrimSpace(assetID))
	if parseErr != nil {
		return "", fmt.Errorf("invalid asset id")
	}
	qctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var cs *string
	err = h.deps.Pool.QueryRow(qctx, `SELECT cert_serial FROM assets WHERE id = $1 LIMIT 1`, id).Scan(&cs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("asset not found")
		}
		return "", fmt.Errorf("asset lookup failed")
	}
	if cs == nil || strings.TrimSpace(*cs) == "" {
		return "", fmt.Errorf("asset has no certificate serial")
	}
	return strings.TrimSpace(*cs), nil
}

func (h *filesHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if rawPath == "" {
		writeTicketsError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}
	path, err := url.PathUnescape(rawPath)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid path encoding")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), fileProxyHTTPTimeout)
	defer cancel()

	certSerial, err := h.resolveAsset(ctx, r.PathValue("id"))
	if err != nil {
		writeTicketsError(w, http.StatusNotFound, err.Error())
		return
	}

	requestID := uuid.NewString()
	ch, unregister := h.deps.RegisterFSDownloadWaiter(certSerial, requestID)
	defer unregister()

	if !h.deps.DispatchJSON(certSerial, map[string]any{
		"action":      "fs_download",
		"request_id":  requestID,
		"path":        path,
		"chunk_size": maxFileProxyChunk,
	}) {
		writeTicketsError(w, http.StatusServiceUnavailable, "agent is not connected")
		return
	}

	base := filepath.Base(path)
	if base == "." || base == "/" {
		base = "download"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, base))
	w.WriteHeader(http.StatusOK)

	fl, _ := w.(http.Flusher)

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if chunk.Err != "" {
				h.deps.Logger.Warn("file download chunk error", "err", chunk.Err, "request_id", requestID)
				return
			}
			if len(chunk.Data) > 0 {
				if _, werr := w.Write(chunk.Data); werr != nil {
					return
				}
				if fl != nil {
					fl.Flush()
				}
			}
			if chunk.EOF {
				return
			}
		}
	}
}

func (h *filesHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if rawPath == "" {
		writeTicketsError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}
	path, err := url.PathUnescape(rawPath)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid path encoding")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), fileProxyHTTPTimeout)
	defer cancel()

	certSerial, err := h.resolveAsset(ctx, r.PathValue("id"))
	if err != nil {
		writeTicketsError(w, http.StatusNotFound, err.Error())
		return
	}

	requestID := uuid.NewString()
	resCh, unregister := h.deps.RegisterFSUploadResultWaiter(certSerial, requestID)
	defer unregister()

	if !h.deps.DispatchJSON(certSerial, map[string]any{
		"action":     "fs_upload_begin",
		"request_id": requestID,
		"path":       path,
	}) {
		writeTicketsError(w, http.StatusServiceUnavailable, "agent is not connected")
		return
	}

	buf := make([]byte, fileProxyReadBodyBuf)
	var total int64
	for {
		n, rerr := r.Body.Read(buf)
		if n > 0 {
			if int64(n) > maxUploadBytesPerCall-total {
				_ = h.deps.DispatchJSON(certSerial, map[string]any{
					"action":     "fs_upload_abort",
					"request_id": requestID,
				})
				writeTicketsError(w, http.StatusRequestEntityTooLarge, "upload exceeds limit")
				return
			}
			total += int64(n)
			payload := map[string]any{
				"action":     "fs_upload_chunk",
				"request_id": requestID,
				"data_b64":   base64.StdEncoding.EncodeToString(buf[:n]),
			}
			if !h.deps.DispatchJSON(certSerial, payload) {
				writeTicketsError(w, http.StatusServiceUnavailable, "agent send failed")
				return
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			h.deps.Logger.Error("upload read body", "err", rerr)
			_ = h.deps.DispatchJSON(certSerial, map[string]any{
				"action":     "fs_upload_abort",
				"request_id": requestID,
			})
			writeTicketsError(w, http.StatusBadRequest, "could not read request body")
			return
		}
	}

	if !h.deps.DispatchJSON(certSerial, map[string]any{
		"action":     "fs_upload_finish",
		"request_id": requestID,
	}) {
		writeTicketsError(w, http.StatusServiceUnavailable, "agent send failed")
		return
	}

	select {
	case <-ctx.Done():
		writeTicketsError(w, http.StatusRequestTimeout, "upload timed out")
	case res := <-resCh:
		if res.OK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bytes_written": total})
			return
		}
		msg := strings.TrimSpace(res.Error)
		if msg == "" {
			msg = "upload failed"
		}
		writeTicketsError(w, http.StatusBadGateway, msg)
	}
}

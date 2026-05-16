package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// RegisterEmbeddedStaticUI serves the embedded React production bundle at the URL root.
// Unknown GET paths fall back to index.html so React Router client-side routes work.
func RegisterEmbeddedStaticUI(mux *http.ServeMux, logger *slog.Logger) {
	root, err := embeddedStaticAssetsFS()
	if err != nil {
		if logger != nil {
			logger.Error("embedded static UI filesystem init failed", "err", err)
		}
		return
	}
	mux.Handle("GET /{path...}", newEmbeddedStaticHandler(root))
}

func newEmbeddedStaticHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Path
		if path == "/v1" || strings.HasPrefix(path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		rel := strings.TrimPrefix(path, "/")
		if rel != "" {
			if _, err := root.Open(rel); err != nil && errors.Is(err, fs.ErrNotExist) {
				rr := *r
				uu := *r.URL
				uu.Path = "/"
				uu.RawQuery = ""
				rr.URL = &uu
				fileServer.ServeHTTP(w, &rr)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

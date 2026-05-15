package serverinstall

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

func newDashboardHandler() (http.Handler, error) {
	root, err := dashboardRootFS()
	if err != nil {
		return nil, err
	}
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
	}), nil
}

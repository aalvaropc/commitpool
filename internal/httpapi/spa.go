package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func spaHandler(dist fs.FS) http.Handler {
	var files http.Handler
	if dist != nil {
		files = http.FileServerFS(dist)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, errMethodRefused)
			return
		}
		if dist == nil {
			http.Error(w, "web app is not built: run `make web-build`", http.StatusNotFound)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && !hidden(name) {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "web app is not built: run `make web-build`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

func hidden(name string) bool {
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

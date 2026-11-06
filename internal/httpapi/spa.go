package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

type previewLookup func(ctx context.Context, shareToken string) (invitePreview, error)

func spaHandler(dist fs.FS, preview previewLookup) http.Handler {
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
		if private(name) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		if token, ok := strings.CutPrefix(name, "w/"); ok && preview != nil && validToken(token) {
			if p, err := preview(r.Context(), token); err == nil {
				index = injectPreview(index, p)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

func private(name string) bool {
	return strings.HasPrefix(name, "p/") || strings.HasPrefix(name, "owner/")
}

func hidden(name string) bool {
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

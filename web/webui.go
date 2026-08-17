// Package webui serves the embedded Kairos operations console.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler returns an SPA-aware handler backed by the production Vite build.
func Handler() http.Handler {
	dist, err := fs.Sub(assets, assetRoot)
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if requested == "." {
			requested = "index.html"
		}
		if _, err := fs.Stat(dist, requested); err == nil {
			files.ServeHTTP(writer, request)
			return
		}
		request.URL.Path = "/"
		files.ServeHTTP(writer, request)
	})
}

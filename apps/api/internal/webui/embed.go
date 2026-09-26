// Package webui embeds the built frontend and serves it as a single-page
// app, falling back to index.html for client-side routes.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed all:dist
var Dist embed.FS

// Handler serves the embedded frontend rooted at dist/ (not dist itself,
// which also holds the .gitkeep placeholder committed before the frontend
// has been built).
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(Dist, "dist")
	if err != nil {
		return nil, err
	}
	return spaHandler(sub), nil
}

func spaHandler(distFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean(r.URL.Path)
		cleanPath = cleanPath[1:] // drop the leading "/"; fs.Stat wants a relative path
		if cleanPath == "" || cleanPath == "." {
			cleanPath = "index.html"
		}

		if _, err := fs.Stat(distFS, cleanPath); err != nil {
			// Not a real static asset: fall back to index.html so
			// client-side routes resolve. If index.html itself hasn't
			// been built yet (only dist/.gitkeep exists), this serves a
			// clean 404 instead of a panic.
			if _, err := fs.Stat(distFS, "index.html"); err != nil {
				http.NotFound(w, r)
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

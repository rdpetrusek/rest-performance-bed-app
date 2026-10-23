package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The built Angular app, copied in by `make build`. Only .gitkeep is there in
// a fresh checkout, so the server then shows a placeholder.
//
//go:embed all:static
var embedded embed.FS

// appFS returns the built app embedded in this binary, or nil if it was built
// without one.
func appFS() fs.FS {
	app, err := fs.Sub(embedded, "static/app")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(app, "index.html"); err != nil {
		return nil
	}
	return app
}

// appHandler serves the Angular app. A path that is not a file is one of the
// app's own routes, so it gets index.html. Hashed bundles are cached for
// good; index.html never is, so a new build shows up on the next load.
func appHandler() func() (bool, http.Handler) {
	static := appFS()
	if static == nil {
		placeholder := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<!doctype html><title>ReST Bed Dashboard</title><p>The web app is not built into this binary. Run <code>make build</code>.</p>`))
		})
		return func() (bool, http.Handler) { return false, placeholder }
	}

	files := http.FileServerFS(static)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(static, name); err != nil || st.IsDir() {
			name = "index.html"
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, static, "index.html")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
	return func() (bool, http.Handler) { return true, handler }
}

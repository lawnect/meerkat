package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets/site
var assets embed.FS

func Handler() http.Handler {
	site, err := fs.Sub(assets, "assets/site")
	if err != nil {
		panic("dashboard assets are unavailable: " + err.Error())
	}
	files := http.FileServer(http.FS(site))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFileFS(w, r, site, "index.html")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "-") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

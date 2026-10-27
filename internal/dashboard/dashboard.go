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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "same-origin")
		// Korean pages use the same routes under /ko.
		page := r.URL.Path
		if page == "/ko" || strings.HasPrefix(page, "/ko/") {
			page = "/" + strings.TrimPrefix(strings.TrimPrefix(page, "/ko"), "/")
		}
		switch page {
		case "/", "/connectors", "/services", "/domains", "/routes", "/access", "/observe", "/diagnostics", "/settings":
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFileFS(w, r, site, "index.html")
			return
		}
		// Detail pages: /connectors/{id} and /domains/{domain}.
		for _, prefix := range []string{"/connectors/", "/domains/"} {
			if strings.HasPrefix(page, prefix) {
				id := strings.TrimSuffix(strings.TrimPrefix(page, prefix), "/")
				if id != "" && !strings.Contains(id, "/") {
					w.Header().Set("Cache-Control", "no-store")
					http.ServeFileFS(w, r, site, "index.html")
					return
				}
			}
		}
		// FileServer would list a directory; only files are served.
		if !strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasSuffix(r.URL.Path, "/") {
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

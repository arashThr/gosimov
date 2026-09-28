// Package ui holds the shared look of the app: the page layout, reusable
// templ components and static assets (CSS, JS, icons).
//
// Feature packages keep their own pages next to their handlers and wrap
// them in Layout.
package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"sync"

	"github.com/arashthr/skeleton/internal/platform/config"
)

//go:embed static
var staticFS embed.FS

// Static is the embedded static directory, rooted at "static/".
var Static, _ = fs.Sub(staticFS, "static")

// Site holds global values the layout needs. It is set once at startup.
var Site = struct {
	Name       string
	Production bool
}{Name: "Skeleton"}

// Setup copies the relevant configuration into Site.
func Setup(cfg *config.Config) {
	Site.Name = cfg.AppName
	Site.Production = cfg.IsProduction()
}

var (
	assetHashesMu sync.Mutex
	assetHashes   = map[string]string{}
)

// Asset returns the URL of a static file with a content hash appended, so
// browsers can cache it forever and still pick up changes after a deploy.
func Asset(path string) string {
	assetHashesMu.Lock()
	defer assetHashesMu.Unlock()
	h, ok := assetHashes[path]
	if !ok {
		b, err := fs.ReadFile(Static, path)
		if err == nil {
			sum := sha256.Sum256(b)
			h = hex.EncodeToString(sum[:])[:10]
		}
		assetHashes[path] = h
	}
	return "/static/" + path + "?v=" + h
}

// StaticHandler serves the embedded assets under /static/.
func StaticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(Static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		files.ServeHTTP(w, r)
	})
}

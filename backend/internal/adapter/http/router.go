// Package http — HTTP-адаптер: роутер, обработчики, раздача встроенного фронтенда.
package http

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

//go:embed all:dist
var distFS embed.FS

// NewRouter возвращает корневой обработчик: /health, /api/* и статика фронтенда.
func NewRouter(reg domain.Registry) http.Handler {
	static, _ := fs.Sub(distFS, "dist")
	return newRouter(static, reg)
}

func newRouter(static fs.FS, reg domain.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("GET /api/characteristics", handleCharacteristics(reg))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.Handle("/", spaHandler(static))
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// spaHandler отдаёт файлы фронтенда, а неизвестные пути — index.html (клиентский роутинг).
func spaHandler(static fs.FS) http.Handler {
	files := http.FileServerFS(static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(static, name); err != nil {
			if _, err := fs.Stat(static, "index.html"); err != nil {
				http.Error(w, "frontend не встроен в сборку", http.StatusNotFound)
				return
			}
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

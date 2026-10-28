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
func NewRouter(reg domain.Registry, traces TraceSource, rooms RoomsSource, cabinet HotelierService, opts ...Option) http.Handler {
	static, _ := fs.Sub(distFS, "dist")
	return newRouter(static, reg, traces, rooms, cabinet, opts...)
}

func newRouter(static fs.FS, reg domain.Registry, traces TraceSource, rooms RoomsSource, cabinet HotelierService, opts ...Option) http.Handler {
	var deps routerDeps
	for _, o := range opts {
		o(&deps)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("GET /api/meta", handleMeta(deps.llmMode, deps.llmModel))
	mux.Handle("GET /api/characteristics", handleCharacteristics(reg))
	if traces != nil {
		mux.Handle("GET /api/hotels/{id}/trace", handleTrace(traces))
	}
	if rooms != nil {
		mux.Handle("GET /api/hotels/{id}/rooms", handleRooms(rooms))
	}
	if deps.search != nil {
		mux.Handle("POST /api/search", handleSearch(deps.search, reg))
		mux.Handle("GET /api/hotels/{id}", handleHotelCard(deps.search, reg))
		mux.Handle("GET /api/hotels/{id}/similar", handleSimilar(deps.search, reg))
	}
	if deps.chat != nil {
		mux.Handle("POST /api/chat", handleChat(deps.chat, reg))
	}
	if deps.examples != nil {
		mux.Handle("GET /api/dialog/examples", handleDialogExamples(deps.examples))
	}
	if deps.guest != nil {
		mux.Handle("POST /api/sessions", handleNewSession(deps.guest))
		mux.Handle("GET /api/sessions/{id}/shortlist", handleShortlist(deps.guest))
		mux.Handle("POST /api/sessions/{id}/shortlist/{hotel_id}", handleShortlistChange(deps.guest, true))
		mux.Handle("DELETE /api/sessions/{id}/shortlist/{hotel_id}", handleShortlistChange(deps.guest, false))
		mux.Handle("POST /api/compare", handleCompare(deps.guest, reg))
		mux.Handle("POST /api/contract", handleContract(deps.guest))
		mux.Handle("POST /api/contract/ask", handleAskHotel(deps.guest))
		mux.Handle("POST /api/bookings", handleBook(deps.guest))
		mux.Handle("GET /api/bookings/{id}", handleGetBooking(deps.guest))
	}
	if deps.admin != nil {
		mux.Handle("GET /api/admin/runs", handleAdminRuns(deps.admin))
		mux.Handle("GET /api/admin/runs/{session}", handleAdminRun(deps.admin))
		mux.Handle("GET /api/admin/conflicts", handleAdminConflicts(deps.admin))
		mux.Handle("GET /api/admin/summary", handleAdminSummary(deps.admin, reg))
		mux.Handle("GET /api/admin/unmet-wishes", handleAdminUnmetWishes(deps.admin))
	}
	if deps.registryAdmin != nil {
		mux.Handle("POST /api/admin/characteristics/draft", handleCharacteristicDraft(deps.registryAdmin, deps.llmMode))
		mux.Handle("POST /api/admin/characteristics", handleCharacteristicSave(deps.registryAdmin))
		mux.Handle("POST /api/admin/characteristics/{key}/compute", handleCharacteristicCompute(deps.registryAdmin))
		mux.Handle("GET /api/admin/jobs/{id}", handleJob(deps.registryAdmin))
	}
	if deps.explain != nil {
		mux.Handle("GET /api/admin/hotels/{id}/facts", handleAdminHotelFacts(deps.explain))
	}
	if cabinet != nil {
		mux.Handle("GET /api/hotelier/hotels", handleHotelierHotels(cabinet))
		mux.Handle("GET /api/hotelier/{hotel_id}/inbox", handleHotelierInbox(cabinet))
		mux.Handle("GET /api/hotelier/{hotel_id}/demand", handleHotelierDemand(cabinet))
		mux.Handle("POST /api/hotelier/hotels/{hotel_id}/facts/{action}", handleHotelierFactAt(cabinet, reg))
		mux.Handle("POST /api/hotelier/questions/{id}/answer", handleHotelierAnswer(cabinet, reg))
		mux.Handle("POST /api/hotelier/questions/{id}/persona-preview", handleHotelierPreview(cabinet))
		mux.Handle("POST /api/hotelier/facts/{id}/{action}", handleHotelierFact(cabinet, reg))
	}
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

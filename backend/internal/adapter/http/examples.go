package http

import (
	"net/http"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
)

// WithDialogExamples подключает GET /api/dialog/examples — первые фразы демо-диалогов для кнопок-примеров в чате.
func WithDialogExamples(ex []dialog.Example) Option {
	return func(d *routerDeps) { d.examples = ex }
}

type exampleDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func handleDialogExamples(ex []dialog.Example) http.HandlerFunc {
	out := make([]exampleDTO, len(ex))
	for i, e := range ex {
		out[i] = exampleDTO{ID: e.ID, Title: e.Title, Text: e.Text}
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"examples": out})
	}
}

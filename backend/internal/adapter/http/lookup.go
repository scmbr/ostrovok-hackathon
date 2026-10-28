package http

import (
	"context"
	"log"
	"net/http"
	"unicode/utf8"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/lookup"
)

// HotelLookup — порт поиска отеля по id или названию для GET /api/hotels/lookup.
type HotelLookup interface {
	Lookup(ctx context.Context, q string) (lookup.Result, error)
}

// WithHotelLookup подключает поиск отеля по названию.
func WithHotelLookup(l HotelLookup) Option { return func(d *routerDeps) { d.lookup = l } }

type lookupHotelDTO struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	District string  `json:"district"`
	Card     cardDTO `json:"card"`
	Score    float64 `json:"score"`
	// Match — «id» (запрос — номер отеля) или «text» (совпали слова названия).
	Match string `json:"match"`
}

func handleHotelLookup(svc HotelLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if utf8.RuneCountInString(q) > lookup.MaxQueryLen {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "запрос слишком длинный для названия отеля"})
			return
		}
		res, err := svc.Lookup(r.Context(), q)
		if err != nil {
			log.Printf("поиск отеля по названию: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось выполнить: поиск отеля по названию"})
			return
		}
		out := struct {
			Query   string           `json:"query"`
			Results []lookupHotelDTO `json:"results"`
			// Unknown — названный гостем отель, которого нет в базе (иначе пусто).
			Unknown string `json:"unknown,omitempty"`
		}{Query: q, Results: []lookupHotelDTO{}, Unknown: res.Unknown}
		for _, m := range res.Matches {
			out.Results = append(out.Results, lookupHotelDTO{ID: m.ID, Name: m.Name, District: m.District, Card: newCardDTO(m.Card),
				Score: m.Score, Match: m.By})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

package http

import (
	"context"
	"log"
	"net/http"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/showcase"
)

// HotelShowcase — порт витрины демо-отелей для GET /api/hotels?demo=1.
type HotelShowcase interface {
	List(ctx context.Context) ([]showcase.Hotel, error)
}

// WithHotelShowcase подключает витрину демо-отелей.
func WithHotelShowcase(s HotelShowcase) Option { return func(d *routerDeps) { d.showcase = s } }

type showcaseHotelDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	District string `json:"district"`
	Stars    int    `json:"stars"`
	PriceEUR int    `json:"price_eur"`
	// SeaView — море видно из окон; false — факт не показываем.
	SeaView bool `json:"sea_view"`
	// WalkToBeachMin — минут пешком до пляжа; 0 — не показываем.
	WalkToBeachMin int  `json:"walk_to_beach_min"`
	Mock           bool `json:"mock"`
}

func handleHotelShowcase(svc HotelShowcase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("demo") != "1" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен параметр demo=1"})
			return
		}
		hs, err := svc.List(r.Context())
		if err != nil {
			log.Printf("витрина демо-отелей: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось выполнить: список демо-отелей"})
			return
		}
		out := struct {
			Hotels []showcaseHotelDTO `json:"hotels"`
		}{Hotels: []showcaseHotelDTO{}}
		for _, h := range hs {
			out.Hotels = append(out.Hotels, showcaseHotelDTO{ID: h.ID, Name: h.Name, District: h.District, Stars: h.Card.Stars,
				PriceEUR: h.Card.PriceEUR, SeaView: h.SeaView, WalkToBeachMin: h.BeachMin, Mock: h.Card.Mock})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

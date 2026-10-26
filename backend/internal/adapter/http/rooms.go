package http

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// RoomsSource — порт категорий номеров для GET /api/hotels/{id}/rooms.
type RoomsSource interface {
	Hotel(ctx context.Context, hotelID int64) (rooms.HotelRooms, error)
}

type roomsDTO struct {
	HotelID    int64         `json:"hotel_id"`
	Facades    []facadeDTO   `json:"facades"`
	Categories []categoryDTO `json:"categories"`
}

type facadeDTO struct {
	ID          int64   `json:"id"`
	AzimuthDeg  float64 `json:"azimuth_deg"`
	Side        string  `json:"side"` // сторона света словами
	FacesStreet bool    `json:"faces_street"`
	Courtyard   bool    `json:"courtyard"`
}

type categoryDTO struct {
	ID              int64             `json:"id"`
	Name            string            `json:"name"`
	RoomsCount      int               `json:"rooms_count"`
	Placements      []placementDTO    `json:"placements"`
	Characteristics []categoryCharDTO `json:"characteristics"`
}

type placementDTO struct {
	FacadeID   int64   `json:"facade_id"`
	FloorFrom  int     `json:"floor_from"`
	FloorTo    int     `json:"floor_to"`
	RoomsCount int     `json:"rooms_count"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	EvidenceID int64   `json:"evidence_id,omitempty"`
}

// categoryCharDTO — характеристика в категории: расчёт по размещению и факт движка правды.
type categoryCharDTO struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	// Target — искомое значение; Probability — доля номеров с ним; Guarantee — все номера категории его имеют.
	Target      string             `json:"target"`
	Probability float64            `json:"probability"`
	Guarantee   bool               `json:"guarantee"`
	Shares      map[string]float64 `json:"shares"`
	Unknown     float64            `json:"unknown"`
	Wish        *wishDTO           `json:"wish,omitempty"`
	Fact        *categoryFactDTO   `json:"fact,omitempty"`
}

type wishDTO struct {
	FacadeID int64   `json:"facade_id"`
	Side     string  `json:"side"`
	MinFloor int     `json:"min_floor"` // 0 — этаж не важен
	Rooms    float64 `json:"rooms"`
	Share    float64 `json:"share"`
	Text     string  `json:"text"`
}

// categoryFactDTO — итог движка правды по категории: значение, уверенность, свидетельства и открытый конфликт.
type categoryFactDTO struct {
	Value            map[string]any `json:"value"`
	Confidence       float64        `json:"confidence"`
	Source           string         `json:"source"`
	Stale            bool           `json:"stale,omitempty"`
	WeakDisagreement bool           `json:"weak_disagreement,omitempty"`
	Conflict         *string        `json:"conflict,omitempty"`
	Evidence         []evidenceDTO  `json:"evidence"`
}

type evidenceDTO struct {
	ID           int64          `json:"id"`
	Source       string         `json:"source"`
	AuthorType   string         `json:"author_type"`
	Claimed      map[string]any `json:"claimed"`
	Reliability  float64        `json:"reliability"`
	Contribution float64        `json:"contribution"`
	ObservedAt   time.Time      `json:"observed_at"`
}

func newRoomsDTO(h rooms.HotelRooms) roomsDTO {
	out := roomsDTO{HotelID: h.HotelID, Facades: []facadeDTO{}, Categories: []categoryDTO{}}
	azimuth := map[int64]float64{}
	for _, f := range h.Facades {
		azimuth[f.ID] = f.AzimuthDeg
		out.Facades = append(out.Facades, facadeDTO{ID: f.ID, AzimuthDeg: math.Round(f.AzimuthDeg), Side: sideRU(f.AzimuthDeg),
			FacesStreet: f.FacesStreet, Courtyard: f.Courtyard})
	}
	for _, c := range h.Categories {
		d := categoryDTO{ID: c.Category.ID, Name: c.Category.Name, RoomsCount: c.Category.Rooms,
			Placements: []placementDTO{}, Characteristics: []categoryCharDTO{}}
		for _, p := range c.Category.Placements {
			d.Placements = append(d.Placements, placementDTO{FacadeID: p.FacadeID, FloorFrom: p.FloorFrom, FloorTo: p.FloorTo,
				RoomsCount: p.Rooms, Source: p.Source, Confidence: p.Confidence, EvidenceID: p.EvidenceID})
		}
		for _, ch := range c.Characteristics {
			a := ch.Assessment
			cd := categoryCharDTO{Key: ch.Key, Title: ch.Title, Target: a.Target, Probability: round2(a.Probability),
				Guarantee: a.Guarantee, Shares: map[string]float64{}, Unknown: round2(a.Unknown)}
			for v, s := range a.Shares {
				cd.Shares[v] = round2(s)
			}
			if w := a.Wish; w != nil {
				side := sideRU(azimuth[w.FacadeID])
				cd.Wish = &wishDTO{FacadeID: w.FacadeID, Side: side, MinFloor: w.MinFloor, Rooms: round2(w.Rooms),
					Share: round2(w.Share), Text: wishText(side, w.MinFloor)}
			}
			if f := ch.Fact; f != nil {
				fd := &categoryFactDTO{Value: f.Fact.Value, Confidence: round2(f.Fact.Confidence), Source: f.Fact.Source,
					Stale: f.Fact.Stale, WeakDisagreement: f.Fact.WeakDisagreement, Evidence: []evidenceDTO{}}
				if f.Fact.Conflict != nil {
					fd.Conflict = &f.Fact.Conflict.Description
				}
				for _, e := range f.Evidence {
					fd.Evidence = append(fd.Evidence, evidenceDTO{ID: e.ID, Source: e.Source, AuthorType: e.AuthorType,
						Claimed: e.Claimed, Reliability: round2(e.Reliability), Contribution: round2(e.Contribution), ObservedAt: e.ObservedAt})
				}
				cd.Fact = fd
			}
			d.Characteristics = append(d.Characteristics, cd)
		}
		out.Categories = append(out.Categories, d)
	}
	return out
}

func wishText(side string, minFloor int) string {
	if minFloor > 0 {
		return fmt.Sprintf("%s сторона, этаж %d+", side, minFloor)
	}
	return side + " сторона"
}

// sideRU — сторона света, в которую смотрит фасад, по азимуту внешней нормали.
func sideRU(az float64) string {
	sides := []string{"северная", "северо-восточная", "восточная", "юго-восточная", "южная", "юго-западная", "западная", "северо-западная"}
	i := int(math.Mod(math.Round(math.Mod(az, 360)/45), 8))
	if i < 0 {
		i += 8
	}
	return sides[i]
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// handleRooms отдаёт категории отеля: размещение, вероятность и гарантию по характеристикам, лучшее пожелание, конфликты.
func handleRooms(src RoomsSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id отеля"})
			return
		}
		h, err := src.Hotel(r.Context(), id)
		if errors.Is(err, rooms.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "у отеля нет категорий номеров"})
			return
		}
		if err != nil {
			log.Printf("категории номеров, отель %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось прочитать категории номеров"})
			return
		}
		writeJSON(w, http.StatusOK, newRoomsDTO(h))
	}
}

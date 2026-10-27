package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// SearchService — порт поиска для /api/search, /api/hotels/{id} и /api/hotels/{id}/similar.
type SearchService interface {
	Search(ctx context.Context, q domain.Query) (search.Result, error)
	Hotel(ctx context.Context, id int64) (search.HotelCard, error)
	Similar(ctx context.Context, id int64, limit int, weights map[string]float64) ([]search.SimilarHotel, error)
}

// Option дополняет роутер необязательными обработчиками.
type Option func(*routerDeps)

type routerDeps struct {
	search SearchService
	chat   ChatService
	guest  GuestFlow
}

// WithSearch подключает поиск.
func WithSearch(s SearchService) Option { return func(d *routerDeps) { d.search = s } }

const maxSearchBody = 64 << 10

type cardDTO struct {
	Mock      bool `json:"mock"`
	Stars     int  `json:"stars"`
	PriceEUR  int  `json:"price_eur"`
	Breakfast bool `json:"breakfast"`
	Wifi      bool `json:"wifi"`
}

func newCardDTO(c domain.Card) cardDTO {
	return cardDTO{Mock: c.Mock, Stars: c.Stars, PriceEUR: c.PriceEUR, Breakfast: c.Breakfast, Wifi: c.Wifi}
}

type sourceDTO struct {
	ID           int64     `json:"id"`
	Source       string    `json:"source"`
	AuthorType   string    `json:"author_type"`
	Reliability  float64   `json:"reliability"`
	Contribution float64   `json:"contribution"`
	ObservedAt   time.Time `json:"observed_at"`
}

type bookingWishDTO struct {
	FacadeID       int64   `json:"facade_id"`
	Side           string  `json:"side"`
	MinFloor       int     `json:"min_floor"`
	Rooms          float64 `json:"rooms"`
	Share          float64 `json:"share"`
	Probability    float64 `json:"probability"`
	HotelConfirmed bool    `json:"hotel_confirmed"`
	Text           string  `json:"text"`
}

// wishEvalDTO — объяснение одного желания: статус, значение, уверенность, источники, конфликт, оговорка.
type wishEvalDTO struct {
	Key         string          `json:"key"`
	Title       string          `json:"title"`
	Importance  string          `json:"importance"`
	Status      string          `json:"status"`
	Value       any             `json:"value,omitempty"`
	Confidence  float64         `json:"confidence"`
	PBase       float64         `json:"p_base"`
	PWish       float64         `json:"p_wish"`
	Probability float64         `json:"probability"`
	Score       float64         `json:"score"`
	Guarantee   bool            `json:"guarantee"`
	Conflict    string          `json:"conflict,omitempty"`
	Caveat      string          `json:"caveat,omitempty"`
	BookingWish *bookingWishDTO `json:"booking_wish,omitempty"`
	FactID      int64           `json:"fact_id,omitempty"`
	ObservedAt  *time.Time      `json:"observed_at,omitempty"`
	ValidUntil  *time.Time      `json:"valid_until,omitempty"`
	Sources     []sourceDTO     `json:"sources"`
}

type variantDTO struct {
	CategoryID int64         `json:"category_id"`
	Name       string        `json:"name"`
	RoomsCount int           `json:"rooms_count"`
	PriceEUR   int           `json:"price_eur"`
	Score      float64       `json:"score"`
	Guaranteed bool          `json:"guaranteed"`
	Violations []string      `json:"violations"`
	Unknown    []string      `json:"unknown"`
	Wishes     []wishEvalDTO `json:"wishes"`
}

type searchHotelDTO struct {
	HotelID     int64       `json:"hotel_id"`
	Name        string      `json:"name"`
	District    string      `json:"district"`
	Card        cardDTO     `json:"card"`
	Score       float64     `json:"score"`
	Violations  []string    `json:"violations"`
	Best        variantDTO  `json:"best"`
	Alternative *variantDTO `json:"alternative,omitempty"`
	// OtherConflicts — открытые конфликты в других категориях отеля по желаниям запроса.
	OtherConflicts []variantConflictDTO `json:"other_conflicts"`
}

type variantConflictDTO struct {
	Category    string `json:"category"`
	Key         string `json:"key"`
	Description string `json:"description"`
}

type searchResponse struct {
	Query      domain.Query     `json:"query"`
	Considered int              `json:"considered"`
	Dropped    int              `json:"dropped"`
	Hotels     []searchHotelDTO `json:"hotels"`
	// GroupsLead — строка над блоками, если «всё сразу» нет или мало; пусто, если блоков компромиссов нет.
	GroupsLead string `json:"groups_lead,omitempty"`
	// Groups — та же выдача по группам: full («всё сразу»), блоки compromise («всё, кроме X»), ranked (блоков нет).
	// Плоский hotels сохранён для совместимости.
	Groups []groupDTO `json:"groups"`
}

// groupDTO — группа выдачи с объяснениями, собранными кодом.
type groupDTO struct {
	Kind         string           `json:"kind"`
	Title        string           `json:"title,omitempty"`
	FailedKey    string           `json:"failed_key,omitempty"`
	MustViolated bool             `json:"must_violated,omitempty"`
	Hotels       []searchHotelDTO `json:"hotels"`
	Explanations []explanationDTO `json:"explanations"`
}

func titleOf(reg domain.Registry, key string) string {
	if c, ok := reg.Get(key); ok {
		return c.Title
	}
	return key
}

func newWishEvalDTO(reg domain.Registry, e domain.WishEval) wishEvalDTO {
	d := wishEvalDTO{Key: e.Key, Title: titleOf(reg, e.Key), Importance: string(e.Importance), Status: string(e.Status), Value: e.Value,
		Confidence: round2(e.Confidence), PBase: round2(e.PBase), PWish: round2(e.PWish), Probability: round2(e.P), Score: round2(e.Score),
		Guarantee: e.Guarantee, Conflict: e.Conflict, Caveat: e.Caveat, FactID: e.FactID, ValidUntil: e.ValidUntil, Sources: []sourceDTO{}}
	if !e.ObservedAt.IsZero() {
		d.ObservedAt = &e.ObservedAt
	}
	if w := e.Wish; w != nil {
		side := sideRU(w.AzimuthDeg)
		d.BookingWish = &bookingWishDTO{FacadeID: w.FacadeID, Side: side, MinFloor: w.MinFloor, Rooms: round2(w.Rooms), Share: round2(w.Share),
			Probability: round2(w.Probability), HotelConfirmed: w.HotelConfirmed, Text: wishText(side, w.MinFloor)}
	}
	for _, s := range e.Sources {
		d.Sources = append(d.Sources, sourceDTO{ID: s.ID, Source: s.Source, AuthorType: s.AuthorType, Reliability: round2(s.Reliability),
			Contribution: round2(s.Contribution), ObservedAt: s.ObservedAt})
	}
	return d
}

func newVariantDTO(reg domain.Registry, v domain.Variant) variantDTO {
	d := variantDTO{CategoryID: v.CategoryID, Name: v.Name, RoomsCount: v.Rooms, PriceEUR: v.PriceEUR, Score: round2(v.Score),
		Guaranteed: v.Guaranteed, Violations: nonNil(v.Violations), Unknown: nonNil(v.Unknown), Wishes: []wishEvalDTO{}}
	for _, e := range v.Evals {
		d.Wishes = append(d.Wishes, newWishEvalDTO(reg, e))
	}
	return d
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func newSearchHotelDTO(reg domain.Registry, h domain.HotelResult) searchHotelDTO {
	d := searchHotelDTO{HotelID: h.HotelID, Name: h.Name, District: h.District, Card: newCardDTO(h.Card), Score: round2(h.Best.Score),
		Violations: nonNil(h.Best.Violations), Best: newVariantDTO(reg, h.Best), OtherConflicts: []variantConflictDTO{}}
	for _, c := range h.OtherConflicts {
		d.OtherConflicts = append(d.OtherConflicts, variantConflictDTO{Category: c.Category, Key: c.Key, Description: c.Description})
	}
	if h.Alt != nil {
		alt := newVariantDTO(reg, *h.Alt)
		d.Alternative = &alt
	}
	return d
}

func newSearchResponse(reg domain.Registry, r search.Result) searchResponse {
	out := searchResponse{Query: r.Query, Considered: r.Considered, Dropped: r.Dropped, Hotels: []searchHotelDTO{}, Groups: []groupDTO{},
		GroupsLead: domain.GroupLead(reg, r.Query.Wishes, r.Groups)}
	for _, h := range r.Hotels {
		out.Hotels = append(out.Hotels, newSearchHotelDTO(reg, h))
	}
	rank := 0
	for _, g := range r.Groups {
		gd := groupDTO{Kind: string(g.Kind), Title: domain.GroupTitle(reg, g), FailedKey: g.FailedKey, MustViolated: g.MustViolated,
			Hotels: []searchHotelDTO{}, Explanations: []explanationDTO{}}
		for _, h := range g.Hotels {
			rank++
			x := domain.ExplainHotel(reg, rank, h)
			gd.Hotels = append(gd.Hotels, newSearchHotelDTO(reg, h))
			gd.Explanations = append(gd.Explanations, explanationDTO{HotelID: x.HotelID, Headline: x.Headline, Lines: nonNil(x.Lines), Notes: nonNil(x.Notes)})
		}
		out.Groups = append(out.Groups, gd)
	}
	return out
}

func writeSearchError(w http.ResponseWriter, err error, what string) {
	var ve *search.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg})
	case errors.Is(err, search.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "отель не найден"})
	default:
		log.Printf("%s: %v", what, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось выполнить: " + what})
	}
}

func handleSearch(svc SearchService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var q domain.Query
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSearchBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&q); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный запрос: " + err.Error()})
			return
		}
		res, err := svc.Search(r.Context(), q)
		if err != nil {
			writeSearchError(w, err, "поиск")
			return
		}
		writeJSON(w, http.StatusOK, newSearchResponse(reg, res))
	}
}

type factDTO struct {
	ID               int64          `json:"id"`
	Key              string         `json:"key"`
	Title            string         `json:"title"`
	EntityType       string         `json:"entity_type"`
	Value            map[string]any `json:"value"`
	Confidence       float64        `json:"confidence"`
	Source           string         `json:"source"`
	Method           string         `json:"method"`
	ObservedAt       time.Time      `json:"observed_at"`
	ValidUntil       *time.Time     `json:"valid_until,omitempty"`
	Stale            bool           `json:"stale,omitempty"`
	WeakDisagreement bool           `json:"weak_disagreement,omitempty"`
	Conflict         string         `json:"conflict,omitempty"`
	Evidence         []evidenceDTO  `json:"evidence"`
}

type hotelCardDTO struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	District string    `json:"district"`
	Lat      float64   `json:"lat"`
	Lon      float64   `json:"lon"`
	Card     cardDTO   `json:"card"`
	Facts    []factDTO `json:"facts"`
	Rooms    *roomsDTO `json:"rooms"`
}

func handleHotelCard(svc SearchService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := hotelID(w, r)
		if !ok {
			return
		}
		h, err := svc.Hotel(r.Context(), id)
		if err != nil {
			writeSearchError(w, err, "чтение карточки отеля")
			return
		}
		out := hotelCardDTO{ID: h.ID, Name: h.Name, District: h.District, Lat: h.Lat, Lon: h.Lon, Card: newCardDTO(h.Card), Facts: []factDTO{}}
		for _, fv := range h.Facts {
			f := fv.Fact
			d := factDTO{ID: f.ID, Key: f.Key, Title: titleOf(reg, f.Key), EntityType: f.EntityType, Value: f.Value, Confidence: round2(f.Confidence),
				Source: f.Source, Method: f.Method, ObservedAt: f.ObservedAt, ValidUntil: f.ValidUntil, Stale: f.Stale,
				WeakDisagreement: f.WeakDisagreement, Evidence: []evidenceDTO{}}
			if f.Conflict != nil {
				d.Conflict = f.Conflict.Description
			}
			for _, e := range fv.Evidence {
				d.Evidence = append(d.Evidence, evidenceDTO{ID: e.ID, Source: e.Source, AuthorType: e.AuthorType, Claimed: e.Claimed,
					Reliability: round2(e.Reliability), Contribution: round2(e.Contribution), ObservedAt: e.ObservedAt})
			}
			out.Facts = append(out.Facts, d)
		}
		if h.Rooms != nil {
			rd := newRoomsDTO(*h.Rooms)
			out.Rooms = &rd
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func hotelID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id отеля"})
		return 0, false
	}
	return id, true
}

type differenceDTO struct {
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	Distance float64 `json:"distance"`
	Weight   float64 `json:"weight"`
	Unknown  bool    `json:"unknown,omitempty"`
}

type similarHotelDTO struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	District    string          `json:"district"`
	Card        cardDTO         `json:"card"`
	Distance    float64         `json:"distance"`
	Differences []differenceDTO `json:"differences"`
}

// maxDifferences — сколько главных отличий показывает «похожий».
const maxDifferences = 3

// parseWeights разбирает weights=ключ:вес,ключ:вес.
func parseWeights(s string) (map[string]float64, bool) {
	out := map[string]float64{}
	if s == "" {
		return out, true
	}
	for _, p := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(p, ":")
		w, err := strconv.ParseFloat(v, 64)
		if !ok || k == "" || err != nil || w <= 0 {
			return nil, false
		}
		out[k] = w
	}
	return out, true
}

func handleSimilar(svc SearchService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := hotelID(w, r)
		if !ok {
			return
		}
		limit := 5
		if s := r.URL.Query().Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 || n > domain.MaxSearchLimit {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit должен быть от 1 до " + strconv.Itoa(domain.MaxSearchLimit)})
				return
			}
			limit = n
		}
		weights, ok := parseWeights(r.URL.Query().Get("weights"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "weights: ожидается ключ:вес,ключ:вес с положительными весами"})
			return
		}
		sims, err := svc.Similar(r.Context(), id, limit, weights)
		if err != nil {
			writeSearchError(w, err, "поиск похожих отелей")
			return
		}
		out := struct {
			HotelID int64             `json:"hotel_id"`
			Similar []similarHotelDTO `json:"similar"`
		}{HotelID: id, Similar: []similarHotelDTO{}}
		for _, s := range sims {
			out.Similar = append(out.Similar, newSimilarDTO(reg, s))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func newSimilarDTO(reg domain.Registry, s search.SimilarHotel) similarHotelDTO {
	d := similarHotelDTO{ID: s.ID, Name: s.Name, District: s.District, Card: newCardDTO(s.Card), Distance: round2(s.Distance),
		Differences: []differenceDTO{}}
	for _, dim := range s.Dims {
		if dim.Distance == 0 || len(d.Differences) == maxDifferences {
			continue
		}
		d.Differences = append(d.Differences, differenceDTO{Key: dim.Key, Title: titleOf(reg, dim.Key), Distance: round2(dim.Distance),
			Weight: dim.Weight, Unknown: dim.Unknown})
	}
	return d
}

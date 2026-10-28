package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
)

// GuestFlow — порт гостевого пути: шортлист, сравнение, контракт ожиданий, «спросить отель», бронь (booking.Service).
type GuestFlow interface {
	NewSession(ctx context.Context) (string, error)
	Shortlist(ctx context.Context, sessionID string) ([]booking.Shown, error)
	AddToShortlist(ctx context.Context, sessionID string, hotelID int64) ([]booking.Shown, error)
	RemoveFromShortlist(ctx context.Context, sessionID string, hotelID int64) ([]booking.Shown, error)
	Compare(ctx context.Context, req booking.CompareRequest) (booking.Compared, error)
	Contract(ctx context.Context, req booking.ContractRequest) (booking.Contract, error)
	AskHotel(ctx context.Context, req booking.AskRequest) (booking.AskResult, error)
	Book(ctx context.Context, req booking.BookRequest) (booking.Booked, error)
	Booking(ctx context.Context, id int64) (booking.BookingView, error)
}

// WithGuestFlow подключает шортлист, сравнение, контракт и бронь.
func WithGuestFlow(g GuestFlow) Option { return func(d *routerDeps) { d.guest = g } }

const maxGuestBody = 16 << 10

// fail отвечает ошибкой гостевого пути: некорректный запрос — 400, нет объекта — 404, остальное — 500.
func failGuest(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, booking.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, booking.ErrSessionNotFound):
		// code отличает потерянную сессию от «нет отеля»: только в первом случае клиент заводит новую сессию.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "session_not_found"})
	case errors.Is(err, booking.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		log.Printf("%s: %v", what, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось выполнить: " + what})
	}
}

func decodeGuest(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGuestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный запрос: " + err.Error()})
		return false
	}
	return true
}

// handleNewSession создаёт пустую сессию: клиент вызывает её, когда сервер ответил 404 на его session_id.
func handleNewSession(g GuestFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := g.NewSession(r.Context())
		if err != nil {
			failGuest(w, err, "сессия")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"session_id": id})
	}
}

// Шортлист.

type shortlistDTO struct {
	SessionID string     `json:"session_id"`
	Hotels    []shownDTO `json:"hotels"`
}

func newShortlistDTO(session string, list []booking.Shown) shortlistDTO {
	out := shortlistDTO{SessionID: session, Hotels: []shownDTO{}}
	for _, s := range list {
		out.Hotels = append(out.Hotels, shownDTO{ID: s.ID, Name: s.Name})
	}
	return out
}

func handleShortlist(g GuestFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := g.Shortlist(r.Context(), r.PathValue("id"))
		if err != nil {
			failGuest(w, err, "шортлист")
			return
		}
		writeJSON(w, http.StatusOK, newShortlistDTO(r.PathValue("id"), list))
	}
}

func handleShortlistChange(g GuestFlow, add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hotelID, err := strconv.ParseInt(r.PathValue("hotel_id"), 10, 64)
		if err != nil || hotelID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hotel_id должен быть числом"})
			return
		}
		session := r.PathValue("id")
		var list []booking.Shown
		if add {
			list, err = g.AddToShortlist(r.Context(), session, hotelID)
		} else {
			list, err = g.RemoveFromShortlist(r.Context(), session, hotelID)
		}
		if err != nil {
			failGuest(w, err, "шортлист")
			return
		}
		writeJSON(w, http.StatusOK, newShortlistDTO(session, list))
	}
}

// Сравнение.

type compareRequest struct {
	SessionID   string          `json:"session_id"`
	Wishes      []domain.Desire `json:"wishes,omitempty"`
	HotelIDs    []int64         `json:"hotel_ids,omitempty"`
	HotelID     int64           `json:"hotel_id,omitempty"`
	CategoryIDs []int64         `json:"category_ids,omitempty"`
}

type compareSideDTO struct {
	HotelID    int64    `json:"hotel_id"`
	Label      string   `json:"label"`
	CategoryID int64    `json:"category_id"`
	Category   string   `json:"category"`
	PriceEUR   int      `json:"price_eur"`
	Score      float64  `json:"score"`
	Guaranteed bool     `json:"guaranteed"`
	Violations []string `json:"violations"`
	Conflicts  []string `json:"conflicts"`
}

type compareDiffDTO struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	DayPart    string   `json:"day_part"`
	Importance string   `json:"importance"`
	Better     int      `json:"better"`
	Texts      []string `json:"texts"`
	Short      []string `json:"short"`
}

type compareUncertainDTO struct {
	Side    int    `json:"side"`
	HotelID int64  `json:"hotel_id"`
	Key     string `json:"key"`
	Title   string `json:"title"`
	Reason  string `json:"reason"`
	// Action — что можно сделать: уточнить у отеля (POST /api/contract/ask).
	Action string `json:"action"`
}

type dayGroupDTO struct {
	Part  string   `json:"part"`
	Title string   `json:"title"`
	Keys  []string `json:"keys"`
}

// compareDTO — сравнение в порядке подачи: вывод → нарушения → различия → цена компромисса → неуверенное.
type compareDTO struct {
	Categories  bool                  `json:"categories"`
	HotelID     int64                 `json:"hotel_id,omitempty"`
	Wishes      []string              `json:"wishes"`
	Winner      int                   `json:"winner"`
	Verdict     string                `json:"verdict"`
	Sides       []compareSideDTO      `json:"sides"`
	Violations  [][]string            `json:"violations"`
	Differences []compareDiffDTO      `json:"differences"`
	Same        []compareDiffDTO      `json:"same"`
	Tradeoff    string                `json:"tradeoff"`
	Uncertain   []compareUncertainDTO `json:"uncertain"`
	Days        []dayGroupDTO         `json:"days"`
}

func newDiffDTO(d domain.CompareDifference) compareDiffDTO {
	return compareDiffDTO{Key: d.Key, Title: d.Title, DayPart: d.DayPart, Importance: string(d.Importance), Better: d.Better,
		Texts: nonNil(d.Texts), Short: nonNil(d.Short)}
}

func newCompareDTO(reg domain.Registry, c booking.Compared) compareDTO {
	cmp := c.Comparison
	out := compareDTO{Categories: c.Categories, HotelID: c.HotelID, Winner: cmp.Winner, Verdict: cmp.Verdict, Tradeoff: cmp.Tradeoff,
		Wishes: []string{}, Sides: []compareSideDTO{}, Violations: [][]string{}, Differences: []compareDiffDTO{}, Same: []compareDiffDTO{},
		Uncertain: []compareUncertainDTO{}, Days: []dayGroupDTO{}}
	for _, w := range c.Query.Wishes {
		out.Wishes = append(out.Wishes, w.Key)
	}
	for _, s := range cmp.Sides {
		sd := compareSideDTO{HotelID: s.HotelID, Label: s.Label, CategoryID: s.Variant.CategoryID, Category: s.Variant.Name,
			PriceEUR: s.Variant.PriceEUR, Score: round2(s.Variant.Score), Guaranteed: s.Variant.Guaranteed, Violations: []string{}, Conflicts: []string{}}
		for _, k := range s.Variant.Violations {
			sd.Violations = append(sd.Violations, titleOf(reg, k))
		}
		for _, oc := range s.Conflicts {
			sd.Conflicts = append(sd.Conflicts, "категория «"+oc.Category+"»: "+oc.Description)
		}
		out.Sides = append(out.Sides, sd)
	}
	for _, v := range cmp.Violations {
		out.Violations = append(out.Violations, nonNil(v))
	}
	for _, d := range cmp.Differences {
		out.Differences = append(out.Differences, newDiffDTO(d))
	}
	for _, d := range cmp.Same {
		out.Same = append(out.Same, newDiffDTO(d))
	}
	for _, u := range cmp.Uncertain {
		out.Uncertain = append(out.Uncertain, compareUncertainDTO{Side: u.Side, HotelID: u.HotelID, Key: u.Key, Title: u.Title, Reason: u.Reason, Action: "ask_hotel"})
	}
	for _, g := range cmp.Days {
		out.Days = append(out.Days, dayGroupDTO{Part: g.Part, Title: g.Title, Keys: nonNil(g.Keys)})
	}
	return out
}

func handleCompare(g GuestFlow, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req compareRequest
		if !decodeGuest(w, r, &req) {
			return
		}
		c, err := g.Compare(r.Context(), booking.CompareRequest{SessionID: req.SessionID, Wishes: req.Wishes, HotelIDs: req.HotelIDs,
			HotelID: req.HotelID, CategoryIDs: req.CategoryIDs})
		if err != nil {
			failGuest(w, err, "сравнение")
			return
		}
		writeJSON(w, http.StatusOK, newCompareDTO(reg, c))
	}
}

// Контракт ожиданий.

type contractRequest struct {
	SessionID  string          `json:"session_id"`
	Wishes     []domain.Desire `json:"wishes,omitempty"`
	HotelID    int64           `json:"hotel_id"`
	CategoryID int64           `json:"category_id"`
	// Wish — пожелание к брони: контракт считается с пожеланием.
	Wish *wishRequest `json:"wish,omitempty"`
}

// wishRequest — пожелание к номеру, как его предложила выдача (booking_wish): фасад и этаж.
type wishRequest struct {
	FacadeID  int64 `json:"facade_id"`
	FloorFrom int   `json:"floor_from"`
}

type offerDTO struct {
	FacadeID       int64   `json:"facade_id"`
	Side           string  `json:"side"`
	FloorFrom      int     `json:"floor_from"`
	Probability    float64 `json:"probability"`
	Text           string  `json:"text"`
	HotelConfirmed bool    `json:"hotel_confirmed"`
	HotelRefused   bool    `json:"hotel_refused"`
}

type contractLineDTO struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Importance  string  `json:"importance"`
	Kind        string  `json:"kind"`
	Probability float64 `json:"probability"`
	Confidence  float64 `json:"confidence"`
	Text        string  `json:"text"`
	Source      string  `json:"source,omitempty"`
	Conflict    string  `json:"conflict,omitempty"`
	// Action — «ask_hotel» у неуверенных и неизвестных пунктов.
	Action string `json:"action,omitempty"`
}

type contractDTO struct {
	HotelID    int64             `json:"hotel_id"`
	HotelName  string            `json:"hotel_name"`
	CategoryID int64             `json:"category_id"`
	Category   string            `json:"category"`
	PriceEUR   int               `json:"price_eur"`
	WithWish   bool              `json:"with_wish"`
	Offer      *offerDTO         `json:"offer,omitempty"`
	Lines      []contractLineDTO `json:"lines"`
}

func newContractLines(ls []domain.ContractLine) []contractLineDTO {
	out := []contractLineDTO{}
	for _, l := range ls {
		d := contractLineDTO{Key: l.Key, Title: l.Title, Importance: string(l.Importance), Kind: l.Kind, Probability: round2(l.Probability),
			Confidence: round2(l.Confidence), Text: l.Text, Source: l.Source, Conflict: l.Conflict}
		if l.CanAsk {
			d.Action = "ask_hotel"
		}
		out = append(out, d)
	}
	return out
}

func newContractDTO(c booking.Contract) contractDTO {
	out := contractDTO{HotelID: c.HotelID, HotelName: c.HotelName, CategoryID: c.CategoryID, Category: c.Variant.Name, PriceEUR: c.Variant.PriceEUR,
		WithWish: c.WithWish, Lines: newContractLines(c.Lines)}
	if o := c.Offer; o != nil {
		out.Offer = &offerDTO{FacadeID: o.FacadeID, Side: domain.SideRU(o.AzimuthDeg), FloorFrom: o.MinFloor, Probability: round2(o.Probability),
			Text: domain.WishText(*o), HotelConfirmed: o.HotelConfirmed, HotelRefused: o.HotelRefused}
	}
	return out
}

func handleContract(g GuestFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req contractRequest
		if !decodeGuest(w, r, &req) {
			return
		}
		creq := booking.ContractRequest{SessionID: req.SessionID, Wishes: req.Wishes, HotelID: req.HotelID, CategoryID: req.CategoryID}
		if req.Wish != nil {
			creq.WithWish = true
		}
		c, err := g.Contract(r.Context(), creq)
		if err != nil {
			failGuest(w, err, "контракт ожиданий")
			return
		}
		writeJSON(w, http.StatusOK, newContractDTO(c))
	}
}

type askRequest struct {
	SessionID string `json:"session_id"`
	HotelID   int64  `json:"hotel_id"`
	Key       string `json:"key"`
}

type askDTO struct {
	// Message — что сказать гостю; бронь не блокируется ни в одном случае.
	Message    string `json:"message"`
	QuestionID int64  `json:"question_id,omitempty"`
	// Status: created | existing | queued | none.
	Status string `json:"status"`
	Sent   bool   `json:"sent"`
}

func handleAskHotel(g GuestFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req askRequest
		if !decodeGuest(w, r, &req) {
			return
		}
		res, err := g.AskHotel(r.Context(), booking.AskRequest{SessionID: req.SessionID, HotelID: req.HotelID, Key: req.Key})
		if err != nil {
			failGuest(w, err, "вопрос отелю")
			return
		}
		out := askDTO{Message: res.Message, Status: "none", Sent: res.Ask.Sent}
		if q := res.Ask.Question; q != nil {
			out.QuestionID = q.ID
			switch {
			case res.Ask.Created:
				out.Status = "created"
			case res.Ask.Sent:
				out.Status = "existing"
			default:
				out.Status = "queued"
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// Бронь.

type bookingRequest struct {
	SessionID  string          `json:"session_id"`
	Wishes     []domain.Desire `json:"wishes,omitempty"`
	HotelID    int64           `json:"hotel_id"`
	CategoryID int64           `json:"category_id"`
	Wish       *wishRequest    `json:"wish,omitempty"`
}

type placedWishDTO struct {
	Text     string `json:"text"`
	FacadeID int64  `json:"facade_id"`
	// Status: waiting — ждём подтверждения отеля; confirmed — отель подтвердил; refused — отель не гарантирует.
	Status     string `json:"status"`
	QuestionID int64  `json:"question_id,omitempty"`
	// QuestionReused — вопрос отелю уже был открыт, новый не создавался.
	QuestionReused bool `json:"question_reused,omitempty"`
}

type bookingDTO struct {
	ID         int64  `json:"id"`
	HotelID    int64  `json:"hotel_id"`
	HotelName  string `json:"hotel_name,omitempty"`
	CategoryID int64  `json:"category_id"`
	Category   string `json:"category,omitempty"`
	PriceEUR   int    `json:"price_eur,omitempty"`
	// PriceMock — цена из мок-карточки отеля (в интерфейсе подписывается «мок»).
	PriceMock bool              `json:"price_mock,omitempty"`
	Wish      *placedWishDTO    `json:"wish,omitempty"`
	Contract  []contractLineDTO `json:"contract"`
}

func handleBook(g GuestFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req bookingRequest
		if !decodeGuest(w, r, &req) {
			return
		}
		breq := booking.BookRequest{SessionID: req.SessionID, Wishes: req.Wishes, HotelID: req.HotelID, CategoryID: req.CategoryID}
		if req.Wish != nil {
			if req.Wish.FacadeID == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wish.facade_id обязателен"})
				return
			}
			breq.FacadeID, breq.MinFloor = req.Wish.FacadeID, req.Wish.FloorFrom
		}
		b, err := g.Book(r.Context(), breq)
		if err != nil {
			failGuest(w, err, "бронь")
			return
		}
		out := bookingDTO{ID: b.ID, HotelID: b.Contract.HotelID, HotelName: b.Contract.HotelName, CategoryID: b.Contract.CategoryID,
			Category: b.Contract.Variant.Name, PriceEUR: b.Contract.Variant.PriceEUR, PriceMock: b.Contract.Variant.PriceEUR > 0, Contract: newContractLines(b.Contract.Lines)}
		if b.WishText != "" {
			out.Wish = &placedWishDTO{Text: b.WishText, FacadeID: breq.FacadeID, Status: b.WishStatus, QuestionReused: b.Reused}
			if b.Question != nil {
				out.Wish.QuestionID = b.Question.ID
			}
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

func handleGetBooking(g GuestFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id брони должен быть числом"})
			return
		}
		v, err := g.Booking(r.Context(), id)
		if err != nil {
			failGuest(w, err, "бронь")
			return
		}
		rec := v.Record
		out := bookingDTO{ID: rec.ID, HotelID: rec.HotelID, HotelName: rec.HotelName, CategoryID: rec.CategoryID, Category: rec.CategoryName,
			PriceEUR: v.PriceEUR, PriceMock: v.PriceEUR > 0, Contract: newContractLines(rec.Expectations)}
		if rec.Wish != nil {
			text, _ := rec.Wish["text"].(string)
			facade, _ := rec.Wish["facade_id"].(float64)
			out.Wish = &placedWishDTO{Text: text, FacadeID: int64(facade), Status: v.WishStatus, QuestionID: rec.WishQuestionID}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

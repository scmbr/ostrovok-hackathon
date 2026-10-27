package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
)

// HotelierService — порт кабинета отельера (usecase/hotelier.Interviewer).
type HotelierService interface {
	Inbox(ctx context.Context, hotelID int64) (hotelier.Inbox, error)
	AnswerQuestion(ctx context.Context, questionID int64, text string, meta ...hotelier.AnswerMeta) (*hotelier.Outcome, error)
	PreviewQuestion(ctx context.Context, questionID int64) (*hotelier.Preview, error)
	Hotels(ctx context.Context) ([]hotelier.HotelSummary, error)
	MissedDemand(ctx context.Context, hotelID int64) ([]hotelier.MissedDemand, error)
	ConfirmFactAt(ctx context.Context, hotelID int64, a hotelier.FactAddress) (*hotelier.FactOutcome, error)
	CorrectFactAt(ctx context.Context, hotelID int64, a hotelier.FactAddress, value string, details map[string]any) (*hotelier.FactOutcome, error)
	ConfirmFact(ctx context.Context, factID int64) (*hotelier.FactOutcome, error)
	CorrectFact(ctx context.Context, factID int64, value string, details map[string]any) (*hotelier.FactOutcome, error)
}

type inboxDTO struct {
	HotelID int64          `json:"hotel_id"`
	Hotel   string         `json:"hotel"`
	Budget  budgetDTO      `json:"budget"`
	Items   []inboxItemDTO `json:"questions"`
	// Waiting и LowValue — копятся и в дайджест не попадают.
	Waiting  int `json:"waiting"`
	LowValue int `json:"low_value"`
	// WishRequests — подтверждения пожеланий из броней: вне бюджета.
	WishRequests []wishRequestDTO `json:"wish_requests"`
}

type wishRequestDTO struct {
	QuestionID int64          `json:"question_id"`
	BookingID  int64          `json:"booking_id"`
	Category   string         `json:"category"`
	Wishes     map[string]any `json:"wishes"`
	CreatedAt  time.Time      `json:"created_at"`
}

type budgetDTO struct {
	Limit      int `json:"limit"`
	WindowDays int `json:"window_days"`
	Sent       int `json:"sent"`
}

type inboxItemDTO struct {
	ID             int64      `json:"id"`
	Characteristic string     `json:"characteristic"`
	Title          string     `json:"title"`
	Format         string     `json:"format"`
	EntityType     string     `json:"entity_type,omitempty"`
	EntityID       string     `json:"entity_id,omitempty"`
	Question       string     `json:"question"`
	Reason         string     `json:"reason"`
	ReasonKind     string     `json:"reason_kind"`
	Value          float64    `json:"value"`
	Score          scoreDTO   `json:"score"`
	Status         string     `json:"status"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
}

type scoreDTO struct {
	Demand            int     `json:"demand"`
	DemandNorm        float64 `json:"demand_norm"`
	Uncertainty       float64 `json:"uncertainty"`
	Impact            float64 `json:"impact"`
	CategoriesChanged int     `json:"categories_changed"`
	Categories        int     `json:"categories"`
}

func handleHotelierInbox(svc HotelierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("hotel_id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id отеля"})
			return
		}
		in, err := svc.Inbox(r.Context(), id)
		if err != nil {
			hotelierError(w, "дайджест", err)
			return
		}
		out := inboxDTO{HotelID: id, Hotel: in.Hotel.Name, Waiting: in.Waiting, LowValue: in.LowValue, Items: []inboxItemDTO{}, WishRequests: []wishRequestDTO{},
			Budget: budgetDTO{Limit: hotelier.Budget, WindowDays: hotelier.BudgetDays, Sent: in.SentRecent}}
		for _, it := range in.Items {
			q := it.Question
			out.Items = append(out.Items, inboxItemDTO{
				ID: q.ID, Characteristic: q.Key, Title: it.Title, Format: q.Format, EntityType: q.EntityType, EntityID: q.EntityID,
				Question: q.Text, Reason: q.Reason, ReasonKind: it.ReasonKind, Value: q.Score.Value, Status: q.Status, SentAt: q.SentAt,
				Score: scoreDTO{q.Score.Demand, q.Score.DemandNorm, q.Score.Uncertainty, q.Score.Impact, q.Score.CategoriesChanged, q.Score.Categories},
			})
		}
		for _, wr := range in.WishRequests {
			out.WishRequests = append(out.WishRequests, wishRequestDTO{QuestionID: wr.QuestionID, BookingID: wr.BookingID, Category: wr.Category,
				Wishes: wr.Wishes, CreatedAt: wr.CreatedAt})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type answerRequest struct {
	Text string `json:"text"`
	// Answer — короткий ответ yes / no вместо текста.
	Answer string `json:"answer"`
	// Comment — необязательное пояснение к кнопке: не разбирается, сохраняется рядом со свидетельством.
	Comment string `json:"comment"`
}

type outcomeDTO struct {
	QuestionID int64          `json:"question_id"`
	Status     string         `json:"status"`
	Note       string         `json:"note,omitempty"`
	ParsedBy   string         `json:"parsed_by,omitempty"`
	Claimed    map[string]any `json:"claimed,omitempty"`
	// FollowUp — уточняющий вопрос, если ответ не задал значения.
	FollowUp string    `json:"follow_up,omitempty"`
	Trust    *trustDTO `json:"trust,omitempty"`
	// Change — факт до и после ответа (после записи и пересчёта репутации); нет, если свидетельство не записано.
	Change *changeDTO `json:"change,omitempty"`
}

type trustDTO struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Agree  int     `json:"agree"`
	Total  int     `json:"total"`
}

func handleHotelierAnswer(svc HotelierService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id вопроса"})
			return
		}
		var req answerRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается JSON {\"text\": …} или {\"answer\": \"yes|no\"}"})
			return
		}
		text := req.Text
		meta := hotelier.AnswerMeta{Button: req.Answer, Comment: req.Comment}
		switch req.Answer {
		case "":
		case "yes":
			text = "да"
		case "no":
			text = "нет"
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answer: yes или no"})
			return
		}
		out, err := svc.AnswerQuestion(r.Context(), id, text, meta)
		if err != nil {
			hotelierError(w, "ответ", err)
			return
		}
		dto := outcomeDTO{QuestionID: out.Question.ID, Status: out.Status, Note: out.Parsed.Note, ParsedBy: out.Parsed.Mode,
			Claimed: out.Parsed.Claimed, FollowUp: out.Parsed.FollowUp, Change: newChangeDTO(reg, out.Change)}
		if t := out.Trust; t != nil {
			dto.Trust = &trustDTO{round2(t.Before), round2(t.After), t.Agree, t.Total}
		}
		writeJSON(w, http.StatusOK, dto)
	}
}

type previewDTO struct {
	QuestionID int64  `json:"question_id"`
	Persona    string `json:"persona"`
	// Text — ответ персоны; Simulated всегда true: это имитация, в БД ничего не записано.
	Text      string         `json:"answer"`
	Simulated bool           `json:"simulated"`
	Found     bool           `json:"found"`
	Claimed   map[string]any `json:"claimed,omitempty"`
	FollowUp  string         `json:"follow_up,omitempty"`
	Note      string         `json:"note,omitempty"`
	ParsedBy  string         `json:"parsed_by"`
}

func handleHotelierPreview(svc HotelierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id вопроса"})
			return
		}
		pv, err := svc.PreviewQuestion(r.Context(), id)
		if err != nil {
			hotelierError(w, "ответ персоны", err)
			return
		}
		writeJSON(w, http.StatusOK, previewDTO{QuestionID: pv.Question.ID, Persona: pv.Persona, Text: pv.Answer, Simulated: true,
			Found: pv.Parsed.Found, Claimed: pv.Parsed.Claimed, FollowUp: pv.Parsed.FollowUp, Note: pv.Parsed.Note, ParsedBy: pv.Parsed.Mode})
	}
}

type factRequest struct {
	Value   string         `json:"value"`
	Details map[string]any `json:"details"`
}

type factOutcomeDTO struct {
	FactID   int64          `json:"fact_id"`
	Key      string         `json:"characteristic"`
	Action   string         `json:"action"`
	Claimed  map[string]any `json:"claimed,omitempty"`
	Evidence bool           `json:"evidence_written"`
	Recheck  bool           `json:"recheck"`
	Note     string         `json:"note,omitempty"`
	Trust    *trustDTO      `json:"trust,omitempty"`
	Change   *changeDTO     `json:"change,omitempty"`
}

func handleHotelierFact(svc HotelierService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id факта"})
			return
		}
		var out *hotelier.FactOutcome
		switch action := r.PathValue("action"); action {
		case "confirm":
			out, err = svc.ConfirmFact(r.Context(), id)
		case "correct":
			var req factRequest
			if derr := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); derr != nil || req.Value == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается JSON {\"value\": …, \"details\": {…}}"})
				return
			}
			out, err = svc.CorrectFact(r.Context(), id, req.Value, req.Details)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "действие: confirm или correct"})
			return
		}
		if err != nil {
			hotelierError(w, "факт", err)
			return
		}
		dto := factOutcomeDTO{FactID: out.FactID, Key: out.Key, Action: out.Action, Claimed: out.Claimed,
			Evidence: out.Evidence, Recheck: out.Recheck, Note: out.Note}
		if t := out.Trust; t != nil {
			dto.Trust = &trustDTO{t.Before, t.After, t.Agree, t.Total}
		}
		writeJSON(w, http.StatusOK, dto)
	}
}

func hotelierError(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, hotelier.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, hotelier.ErrState), errors.Is(err, hotelier.ErrNoPersona):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, hotelier.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		log.Printf("кабинет отельера, %s: %v", what, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка"})
	}
}

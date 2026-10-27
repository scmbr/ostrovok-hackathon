package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// ChatService — порт агента диалога для POST /api/chat. Ход отдаёт события по мере готовности.
type ChatService interface {
	Turn(ctx context.Context, req dialog.Request, emit func(dialog.Event)) error
}

// WithChat подключает диалог.
func WithChat(c ChatService) Option { return func(d *routerDeps) { d.chat = c } }

const (
	maxChatBody    = 16 << 10
	maxChatMessage = 1000
	maxSessionID   = 64
)

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	// Choice — id кнопки ответа на уточнение (событие clarification).
	Choice string `json:"choice,omitempty"`
	// HotelID — отель, открытый у гостя («похожий на этот»).
	HotelID int64 `json:"hotel_id,omitempty"`
}

type shownDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// chatResultsDTO — событие results: поиск (hotels + explanations) или «похожие» (base + similar).
type chatResultsDTO struct {
	Kind         string            `json:"kind"`
	Query        domain.Query      `json:"query"`
	Considered   int               `json:"considered,omitempty"`
	Dropped      int               `json:"dropped,omitempty"`
	Hotels       []searchHotelDTO  `json:"hotels,omitempty"`
	GroupsLead   string            `json:"groups_lead,omitempty"`
	Groups       []groupDTO        `json:"groups,omitempty"`
	Explanations []explanationDTO  `json:"explanations,omitempty"`
	Base         *shownDTO         `json:"base,omitempty"`
	Similar      []similarHotelDTO `json:"similar,omitempty"`
}

// explanationDTO — объяснение выдачи по отелю, собранное кодом из структуры оценки.
type explanationDTO struct {
	HotelID  int64    `json:"hotel_id"`
	Headline string   `json:"headline"`
	Lines    []string `json:"lines"`
	Notes    []string `json:"notes"`
}

type messageDTO struct {
	Text string `json:"text"`
	// Source: template — текст собран кодом; llm — пересказ модели, прошедший проверку чисел и названий.
	Source string `json:"source"`
}

type doneDTO struct {
	SessionID string `json:"session_id"`
	Mode      string `json:"mode"`
	// ClarificationsAsked — сколько уточнений задано в сессии (лимит 2).
	ClarificationsAsked int `json:"clarifications_asked"`
}

func newChatResultsDTO(reg domain.Registry, e dialog.ResultsEvent) chatResultsDTO {
	out := chatResultsDTO{Kind: e.Kind, Query: e.Query, Considered: e.Considered, Dropped: e.Dropped}
	if e.Kind == "similar" && e.Similar != nil {
		out.Base = &shownDTO{ID: e.Similar.Base.ID, Name: e.Similar.Base.Name}
		out.Similar = []similarHotelDTO{}
		for _, s := range e.Similar.Hotels {
			out.Similar = append(out.Similar, newSimilarDTO(reg, s))
		}
		return out
	}
	sr := newSearchResponse(reg, search.Result{Query: e.Query, Hotels: e.Hotels, Groups: e.Groups})
	out.Hotels, out.GroupsLead, out.Groups = sr.Hotels, sr.GroupsLead, sr.Groups
	out.Explanations = []explanationDTO{}
	for _, x := range e.Explanations {
		out.Explanations = append(out.Explanations, explanationDTO{HotelID: x.HotelID, Headline: x.Headline, Lines: nonNil(x.Lines), Notes: nonNil(x.Notes)})
	}
	return out
}

// sseWriter пишет события text/event-stream; заголовки уходят с первым событием, поэтому ошибка до него
// возвращается обычным JSON с кодом статуса.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started bool
}

func (s *sseWriter) send(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		log.Printf("sse %s: %v", event, err)
		return
	}
	if !s.started {
		h := s.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no")
		s.w.WriteHeader(http.StatusOK)
		s.started = true
	}
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func handleChat(svc ChatService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный запрос: " + err.Error()})
			return
		}
		req.SessionID, req.Message = strings.TrimSpace(req.SessionID), strings.TrimSpace(req.Message)
		switch {
		case req.SessionID == "" || utf8.RuneCountInString(req.SessionID) > maxSessionID:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("session_id обязателен, до %d символов", maxSessionID)})
			return
		case req.Message == "" && req.Choice == "":
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны message или choice"})
			return
		case utf8.RuneCountInString(req.Message) > maxChatMessage:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("сообщение длиннее %d символов", maxChatMessage)})
			return
		}
		flusher, _ := w.(http.Flusher)
		sse := &sseWriter{w: w, flusher: flusher}
		err := svc.Turn(r.Context(), dialog.Request{SessionID: req.SessionID, Message: req.Message, Choice: req.Choice, HotelID: req.HotelID},
			func(e dialog.Event) { emitChat(sse, reg, e) })
		if err == nil {
			return
		}
		var ve *search.ValidationError
		switch {
		case errors.As(err, &ve) && !sse.started:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg})
		case !sse.started:
			log.Printf("диалог: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось выполнить: диалог"})
		default:
			log.Printf("диалог: %v", err)
			sse.send("error", map[string]string{"error": "не удалось выполнить: диалог"})
		}
	}
}

func emitChat(sse *sseWriter, reg domain.Registry, e dialog.Event) {
	switch d := e.Data.(type) {
	case dialog.ResultsEvent:
		sse.send(e.Type, newChatResultsDTO(reg, d))
	case dialog.MessageEvent:
		sse.send(e.Type, messageDTO{Text: d.Text, Source: d.Source})
	case dialog.DoneEvent:
		sse.send(e.Type, doneDTO{SessionID: d.SessionID, Mode: d.Mode, ClarificationsAsked: d.Asked})
	default:
		sse.send(e.Type, e.Data)
	}
}

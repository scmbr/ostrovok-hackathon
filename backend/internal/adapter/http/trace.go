package http

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// TraceSource — порт журнала агента для GET /api/hotels/{id}/trace.
type TraceSource interface {
	ListTrace(ctx context.Context, hotelID int64, sessionID string) ([]agent.TraceStep, error)
}

type traceDTO struct {
	HotelID   int64          `json:"hotel_id"`
	SessionID string         `json:"session_id,omitempty"`
	Mode      string         `json:"mode,omitempty"`
	Steps     []traceStepDTO `json:"steps"`
}

type traceStepDTO struct {
	Step      int            `json:"step"`
	Mode      string         `json:"mode"`
	Decision  string         `json:"decision"`
	Tool      string         `json:"tool,omitempty"`
	Input     map[string]any `json:"input"`
	Output    map[string]any `json:"output"`
	CreatedAt time.Time      `json:"created_at"`
}

// handleTrace отдаёт журнал последнего прогона агента по отелю; ?session=ID — конкретный прогон.
func handleTrace(src TraceSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id отеля"})
			return
		}
		steps, err := src.ListTrace(r.Context(), id, r.URL.Query().Get("session"))
		if err != nil {
			log.Printf("журнал агента, отель %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось прочитать журнал"})
			return
		}
		out := traceDTO{HotelID: id, Steps: []traceStepDTO{}}
		for _, s := range steps {
			out.SessionID = s.SessionID
			out.Mode = s.Mode
			out.Steps = append(out.Steps, traceStepDTO{Step: s.Step, Mode: s.Mode, Decision: s.Decision, Tool: s.Tool,
				Input: s.Input, Output: s.Output, CreatedAt: s.CreatedAt})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/admin"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/explain"
)

// AdminSource — порт экрана «под капотом» (usecase/admin.Service): журнал агентов, конфликты, задачи, устаревшие факты.
type AdminSource interface {
	Runs(ctx context.Context, agentName string, hotelID int64, limit int) ([]admin.Run, error)
	RunSteps(ctx context.Context, sessionID string) ([]agent.TraceStep, error)
	Conflicts(ctx context.Context, f admin.ConflictFilter) ([]admin.ConflictView, error)
	Tasks(ctx context.Context, hotelID int64, limit int) ([]admin.Task, error)
	StaleFacts(ctx context.Context, hotelID int64, limit int) ([]admin.StaleFact, error)
	Summary(ctx context.Context) (admin.Summary, error)
	UnmetWishes(ctx context.Context, limit int) ([]admin.UnmetWish, error)
}

// ExplainSource — порт разбора фактов на свидетельства (usecase/explain.Service).
type ExplainSource interface {
	Hotel(ctx context.Context, hotelID int64) ([]explain.FactView, error)
}

// WithAdmin подключает маршруты /api/admin/* (роль админа без авторизации, как договорено с организаторами).
func WithAdmin(a AdminSource, e ExplainSource) Option {
	return func(d *routerDeps) { d.admin, d.explain = a, e }
}

type runDTO struct {
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	HotelID   int64     `json:"hotel_id,omitempty"`
	Mode      string    `json:"mode"`
	Steps     int       `json:"steps"`
	Refusals  int       `json:"refusals"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Title     string    `json:"title"`
}

func handleAdminRuns(src AdminSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hotelID, ok := optInt(w, r, "hotel_id")
		if !ok {
			return
		}
		limit, ok := optInt(w, r, "limit")
		if !ok {
			return
		}
		runs, err := src.Runs(r.Context(), r.URL.Query().Get("agent"), hotelID, int(limit))
		if err != nil {
			adminError(w, "журнал прогонов", err)
			return
		}
		out := struct {
			Runs []runDTO `json:"runs"`
		}{Runs: []runDTO{}}
		for _, x := range runs {
			out.Runs = append(out.Runs, runDTO{Agent: x.Agent, SessionID: x.SessionID, HotelID: x.HotelID, Mode: x.Mode, Steps: x.Steps,
				Refusals: x.Refusals, StartedAt: x.StartedAt, EndedAt: x.EndedAt, Title: x.Title})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type runStepDTO struct {
	Step     int            `json:"step"`
	Mode     string         `json:"mode"`
	Decision string         `json:"decision"`
	Tool     string         `json:"tool,omitempty"`
	Input    map[string]any `json:"input"`
	Output   map[string]any `json:"output"`
	// Refusal — отказ: попытка нарушить правило агента; причина в decision.
	Refusal   bool      `json:"refusal"`
	CreatedAt time.Time `json:"created_at"`
}

func handleAdminRun(src AdminSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("session")
		if id == "" || len(id) > 128 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный session_id"})
			return
		}
		steps, err := src.RunSteps(r.Context(), id)
		if err != nil {
			adminError(w, "прогон агента", err)
			return
		}
		if len(steps) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "прогон не найден"})
			return
		}
		out := struct {
			SessionID string       `json:"session_id"`
			Agent     string       `json:"agent"`
			HotelID   int64        `json:"hotel_id,omitempty"`
			Steps     []runStepDTO `json:"steps"`
		}{SessionID: id, Agent: steps[0].Agent, HotelID: steps[0].HotelID, Steps: []runStepDTO{}}
		for _, s := range steps {
			out.Steps = append(out.Steps, runStepDTO{Step: s.Step, Mode: s.Mode, Decision: s.Decision, Tool: s.Tool, Input: s.Input, Output: s.Output,
				Refusal: strings.HasPrefix(s.Decision, "отказ"), CreatedAt: s.CreatedAt})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type chainDTO struct {
	Base      float64 `json:"base"`
	Trust     float64 `json:"trust,omitempty"`
	TrustMult float64 `json:"trust_mult"`
	Asymmetry float64 `json:"asymmetry"`
	Freshness float64 `json:"freshness"`
	Product   float64 `json:"product"`
	R         float64 `json:"r"`
	Clamped   bool    `json:"clamped"`
}

type explainedEvidenceDTO struct {
	ID         int64          `json:"id"`
	Source     string         `json:"source"`
	AuthorType string         `json:"author_type"`
	AuthorID   string         `json:"author_id"`
	Claimed    map[string]any `json:"claimed"`
	Comment    string         `json:"comment,omitempty"`
	ObservedAt time.Time      `json:"observed_at"`
	Chain      chainDTO       `json:"chain"`
	// Logit — ln(r / (1 − r)), не меньше нуля; Contribution — подписанный вклад в итог.
	Logit        float64 `json:"logit"`
	Role         string  `json:"role"`
	Contribution float64 `json:"contribution"`
	Group        string  `json:"group,omitempty"`
	ZeroWeight   bool    `json:"zero_weight"`
}

type explainedFactDTO struct {
	EntityType       string                 `json:"entity_type"`
	EntityID         string                 `json:"entity_id"`
	Key              string                 `json:"key"`
	Title            string                 `json:"title"`
	ValueField       string                 `json:"value_field"`
	Value            map[string]any         `json:"value"`
	Confidence       float64                `json:"confidence"`
	S                float64                `json:"s"`
	Sigma            float64                `json:"sigma"`
	Summary          string                 `json:"summary"`
	Stale            bool                   `json:"stale,omitempty"`
	WeakDisagreement bool                   `json:"weak_disagreement,omitempty"`
	Conflict         string                 `json:"conflict,omitempty"`
	Theta            float64                `json:"theta"`
	ThetaLowered     bool                   `json:"theta_lowered,omitempty"`
	Evidence         []explainedEvidenceDTO `json:"evidence"`
}

func newExplainedFactDTO(fv explain.FactView) explainedFactDTO {
	x := fv.Explanation
	d := explainedFactDTO{EntityType: fv.EntityType, EntityID: fv.EntityID, Key: fv.Key, Title: fv.Title, ValueField: x.ValueField,
		Value: x.Fact.Value, Confidence: round3(x.Fact.Confidence), S: round3(x.S), Sigma: round3(x.Sigma), Summary: x.Summary,
		Stale: x.Fact.Stale, WeakDisagreement: x.Fact.WeakDisagreement, Theta: x.Theta, ThetaLowered: x.ThetaLowered, Evidence: []explainedEvidenceDTO{}}
	if x.Fact.Conflict != nil {
		d.Conflict = x.Fact.Conflict.Description
	}
	for _, e := range x.Evidence {
		ch := e.Chain
		comment, _ := e.Evidence.Details["comment"].(string)
		d.Evidence = append(d.Evidence, explainedEvidenceDTO{
			ID: e.Evidence.ID, Source: e.Evidence.Source, AuthorType: e.Evidence.AuthorType, AuthorID: e.Evidence.AuthorID,
			Claimed: e.Evidence.Claimed, Comment: comment, ObservedAt: e.Evidence.ObservedAt,
			Chain: chainDTO{Base: round3(ch.Base), Trust: round3(ch.Trust), TrustMult: round3(ch.TrustMult), Asymmetry: ch.Asymmetry,
				Freshness: round3(ch.Freshness), Product: round3(ch.Product), R: round3(ch.R), Clamped: ch.Product != ch.R},
			Logit: round3(e.Logit), Role: e.Role, Contribution: round3(e.Contribution), Group: e.Group, ZeroWeight: e.ZeroWeight})
	}
	return d
}

func handleAdminHotelFacts(src ExplainSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := hotelID(w, r)
		if !ok {
			return
		}
		fs, err := src.Hotel(r.Context(), id)
		if err != nil {
			adminError(w, "разбор фактов", err)
			return
		}
		out := struct {
			HotelID int64              `json:"hotel_id"`
			Facts   []explainedFactDTO `json:"facts"`
		}{HotelID: id, Facts: []explainedFactDTO{}}
		for _, f := range fs {
			out.Facts = append(out.Facts, newExplainedFactDTO(f))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type sideEvidenceDTO struct {
	AuthorType   string  `json:"author_type"`
	AuthorID     string  `json:"author_id"`
	Source       string  `json:"source"`
	Contribution float64 `json:"contribution"`
	Comment      string  `json:"comment,omitempty"`
	ZeroWeight   bool    `json:"zero_weight,omitempty"`
}

type sideDTO struct {
	Value    string            `json:"value"`
	Weight   float64           `json:"weight"`
	Evidence []sideEvidenceDTO `json:"evidence"`
}

type adminConflictDTO struct {
	ID             int64     `json:"id"`
	EntityType     string    `json:"entity_type"`
	EntityID       string    `json:"entity_id"`
	Key            string    `json:"key"`
	Title          string    `json:"title"`
	HotelID        int64     `json:"hotel_id,omitempty"`
	Status         string    `json:"status"`
	Description    string    `json:"description"`
	Resolution     string    `json:"resolution,omitempty"`
	ResolutionKind string    `json:"resolution_kind,omitempty"`
	DetectedAt     time.Time `json:"detected_at"`
	Sides          []sideDTO `json:"sides"`
	Theta          float64   `json:"theta,omitempty"`
	ThetaLowered   bool      `json:"theta_lowered,omitempty"`
	ThetaReason    string    `json:"theta_reason,omitempty"`
	Question       *struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Format string `json:"format"`
	} `json:"question,omitempty"`
}

type taskDTO struct {
	ID        int64      `json:"id"`
	HotelID   int64      `json:"hotel_id"`
	Key       string     `json:"key"`
	Reason    string     `json:"reason"`
	Status    string     `json:"status"`
	Outcome   string     `json:"outcome,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}

type staleDTO struct {
	EntityType string     `json:"entity_type"`
	EntityID   string     `json:"entity_id"`
	Key        string     `json:"key"`
	HotelID    int64      `json:"hotel_id,omitempty"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
}

func handleAdminConflicts(src AdminSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hotel, ok := optInt(w, r, "hotel_id")
		if !ok {
			return
		}
		status := r.URL.Query().Get("status")
		if status != "" && status != "open" && status != "closed" && status != "all" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status: open, closed или all"})
			return
		}
		cs, err := src.Conflicts(r.Context(), admin.ConflictFilter{Status: status, HotelID: hotel})
		if err != nil {
			adminError(w, "конфликты", err)
			return
		}
		tasks, err := src.Tasks(r.Context(), hotel, 50)
		if err != nil {
			adminError(w, "задачи пересчёта", err)
			return
		}
		stale, err := src.StaleFacts(r.Context(), hotel, 100)
		if err != nil {
			adminError(w, "устаревшие факты", err)
			return
		}
		out := struct {
			Conflicts []adminConflictDTO `json:"conflicts"`
			Tasks     []taskDTO          `json:"tasks"`
			Stale     []staleDTO         `json:"stale"`
		}{Conflicts: []adminConflictDTO{}, Tasks: []taskDTO{}, Stale: []staleDTO{}}
		for _, c := range cs {
			d := adminConflictDTO{ID: c.ID, EntityType: c.EntityType, EntityID: c.EntityID, Key: c.Key, Title: c.Title, HotelID: c.HotelID, Status: c.Status,
				Description: c.Description, Resolution: c.Resolution, ResolutionKind: c.ResolutionKind, DetectedAt: c.DetectedAt,
				Sides: []sideDTO{}, Theta: c.Theta, ThetaLowered: c.ThetaLowered, ThetaReason: c.ThetaReason}
			for _, s := range c.Sides {
				sd := sideDTO{Value: s.Value, Weight: round2(s.Weight), Evidence: []sideEvidenceDTO{}}
				for _, e := range s.Evidence {
					sd.Evidence = append(sd.Evidence, sideEvidenceDTO{AuthorType: e.AuthorType, AuthorID: e.AuthorID, Source: e.Source,
						Contribution: round2(e.Contribution), Comment: e.Comment, ZeroWeight: e.ZeroWeight})
				}
				d.Sides = append(d.Sides, sd)
			}
			if q := c.Question; q != nil {
				d.Question = &struct {
					ID     int64  `json:"id"`
					Status string `json:"status"`
					Format string `json:"format"`
				}{q.ID, q.Status, q.Format}
			}
			out.Conflicts = append(out.Conflicts, d)
		}
		for _, t := range tasks {
			out.Tasks = append(out.Tasks, taskDTO{ID: t.ID, HotelID: t.HotelID, Key: t.Key, Reason: t.Reason, Status: t.Status, Outcome: t.Outcome,
				CreatedAt: t.CreatedAt, DoneAt: t.DoneAt})
		}
		for _, f := range stale {
			out.Stale = append(out.Stale, staleDTO{EntityType: f.EntityType, EntityID: f.EntityID, Key: f.Key, HotelID: f.HotelID, ValidUntil: f.ValidUntil})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type coverageDTO struct {
	Key           string  `json:"key"`
	Hotels        int     `json:"hotels"`
	AvgConfidence float64 `json:"avg_confidence"`
	LowConfidence int     `json:"low_confidence"`
}

func handleAdminSummary(src AdminSource, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sm, err := src.Summary(r.Context())
		if err != nil {
			adminError(w, "сводка", err)
			return
		}
		out := struct {
			DemoHotels    int            `json:"demo_hotels"`
			Coverage      []coverageDTO  `json:"coverage"`
			OpenConflicts int            `json:"open_conflicts"`
			WeakFacts     int            `json:"weak_disagreement_facts"`
			StaleFacts    int            `json:"stale_facts"`
			Questions     map[string]int `json:"questions"`
			HotelsAsked   int            `json:"hotels_asked"`
			// QuestionShare — доля демо-отелей, которым задавали вопрос.
			QuestionShare float64 `json:"question_share"`
		}{DemoHotels: sm.DemoHotels, Coverage: []coverageDTO{}, OpenConflicts: sm.OpenConflicts, WeakFacts: sm.WeakFacts, StaleFacts: sm.StaleFacts,
			Questions: sm.Questions, HotelsAsked: sm.HotelsAsked}
		if sm.DemoHotels > 0 {
			out.QuestionShare = round3(float64(sm.HotelsAsked) / float64(sm.DemoHotels))
		}
		for _, c := range sm.Coverage {
			out.Coverage = append(out.Coverage, coverageDTO{Key: titleOf(reg, c.Key), Hotels: c.Hotels, AvgConfidence: round3(c.AvgConfidence), LowConfidence: c.LowConfidence})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type unmetWishDTO struct {
	Text       string          `json:"text"`
	Normalized string          `json:"normalized"`
	Count      int             `json:"count"`
	FirstSeen  time.Time       `json:"first_seen"`
	LastSeen   time.Time       `json:"last_seen"`
	Proposal   json.RawMessage `json:"proposal"`
}

func handleAdminUnmetWishes(src AdminSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, ok := optInt(w, r, "limit")
		if !ok {
			return
		}
		if limit == 0 || limit > 200 {
			limit = 100
		}
		ws, err := src.UnmetWishes(r.Context(), int(limit))
		if err != nil {
			adminError(w, "неизвестные пожелания", err)
			return
		}
		out := struct {
			Wishes []unmetWishDTO `json:"wishes"`
		}{Wishes: []unmetWishDTO{}}
		for _, x := range ws {
			d := unmetWishDTO{Text: x.Text, Normalized: x.Normalized, Count: x.Count, FirstSeen: x.FirstSeen, LastSeen: x.LastSeen, Proposal: json.RawMessage("null")}
			if len(x.Proposal) > 0 {
				d.Proposal = x.Proposal
			}
			out.Wishes = append(out.Wishes, d)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// optInt разбирает необязательный целый параметр запроса (пусто — 0); ошибка — 400.
func optInt(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": name + " должен быть неотрицательным числом"})
		return 0, false
	}
	return n, true
}

func adminError(w http.ResponseWriter, what string, err error) {
	log.Printf("под капотом, %s: %v", what, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка"})
}

func round3(x float64) float64 { return float64(int64(x*1000+sign(x)*0.5)) / 1000 }

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

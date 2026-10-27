package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

// RegistryAdmin — порт добавления характеристики текстом и расчёта по демо-отелям (без перезапуска сервера).
type RegistryAdmin interface {
	Draft(ctx context.Context, text string) (registry.Draft, error)
	Save(ctx context.Context, yaml string) (domain.Characteristic, error)
	StartCompute(ctx context.Context, key string) (string, error)
	Job(id string) (registry.JobView, bool)
}

// WithRegistryAdmin подключает маршруты добавления характеристик (/api/admin/characteristics*, /api/admin/jobs/*).
func WithRegistryAdmin(a RegistryAdmin) Option { return func(d *routerDeps) { d.registryAdmin = a } }

const maxRegistryBody = 64 << 10

type draftRequest struct {
	Text string `json:"text"`
}

type draftDTO struct {
	YAML     string   `json:"yaml"`
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Synonyms []string `json:"synonyms"`
	Notes    []string `json:"notes"`
	// Errors — что мешает сохранить; YAML можно поправить и проверить заново при сохранении.
	Errors   []string `json:"errors"`
	Attempts int      `json:"attempts"`
	// Mode — откуда ответ: llm (живая модель) или cache (заготовки без ключа).
	Mode string `json:"mode"`
}

func handleCharacteristicDraft(a RegistryAdmin, mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req draftRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRegistryBody)).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается JSON {\"text\": \"описание характеристики\"}"})
			return
		}
		d, err := a.Draft(r.Context(), req.Text)
		switch {
		case errors.Is(err, registry.ErrExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			// Ответ LLM не прошёл проверку, нет заготовки в кеше или сеть: пользователю нужна причина, а не 500.
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		out := draftDTO{YAML: d.YAML, Key: d.Key, Title: d.Title, Synonyms: nonNilStrings(d.Synonyms), Notes: nonNilStrings(d.Notes),
			Errors: nonNilStrings(d.Errors), Attempts: d.Attempts, Mode: mode}
		writeJSON(w, http.StatusOK, out)
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type saveRequest struct {
	YAML string `json:"yaml"`
}

func handleCharacteristicSave(a RegistryAdmin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req saveRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRegistryBody)).Decode(&req); err != nil || strings.TrimSpace(req.YAML) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается JSON {\"yaml\": \"…\"}"})
			return
		}
		c, err := a.Save(r.Context(), req.YAML)
		var ve *registry.ValidationError
		switch {
		case errors.As(err, &ve):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "характеристика не прошла проверку", "errors": ve.Problems})
		case errors.Is(err, registry.ErrExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case err != nil:
			log.Printf("сохранение характеристики: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusCreated, map[string]string{"key": c.Key, "title": c.Title})
		}
	}
}

func handleCharacteristicCompute(a RegistryAdmin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := a.StartCompute(r.Context(), r.PathValue("key"))
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"job_id": id})
	}
}

type jobDTO struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Done       int        `json:"done"`
	Total      int        `json:"total"`
	Message    string     `json:"message,omitempty"`
	Errors     []string   `json:"errors"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func handleJob(a RegistryAdmin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, ok := a.Job(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "задание не найдено"})
			return
		}
		writeJSON(w, http.StatusOK, jobDTO{ID: j.ID, Title: j.Title, Status: j.Status, Done: j.Done, Total: j.Total, Message: j.Message,
			Errors: nonNilStrings(j.Errors), StartedAt: j.StartedAt, FinishedAt: j.FinishedAt})
	}
}

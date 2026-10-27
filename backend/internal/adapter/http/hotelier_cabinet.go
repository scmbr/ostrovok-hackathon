package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
)

// Состояния конфликта в результате ответа (changeDTO.ConflictChange).
const (
	conflictNone    = "none"
	conflictOpened  = "opened"
	conflictClosed  = "closed"
	conflictRemains = "remains"
)

type evidenceSnapDTO struct {
	ID           int64          `json:"id"`
	Source       string         `json:"source"`
	AuthorType   string         `json:"author_type"`
	AuthorTrust  float64        `json:"author_trust"`
	Claimed      map[string]any `json:"claimed"`
	Reliability  float64        `json:"reliability"`
	Contribution float64        `json:"contribution"`
	ObservedAt   time.Time      `json:"observed_at"`
}

type snapDTO struct {
	Found            bool              `json:"found"`
	Value            map[string]any    `json:"value,omitempty"`
	Confidence       float64           `json:"confidence"`
	Conflict         string            `json:"conflict,omitempty"`
	WeakDisagreement bool              `json:"weak_disagreement,omitempty"`
	Stale            bool              `json:"stale,omitempty"`
	Evidence         []evidenceSnapDTO `json:"evidence"`
}

// changeDTO — как ответ или правка изменили факт: срезы до и после, итог по конфликту.
type changeDTO struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Key        string `json:"key"`
	Title      string `json:"title"`
	// ValueField — поле значения в value / claimed (у категории номеров своё).
	ValueField     string  `json:"value_field"`
	Before         snapDTO `json:"before"`
	After          snapDTO `json:"after"`
	ConflictChange string  `json:"conflict_change"`
}

func newSnapDTO(s hotelier.FactSnap) snapDTO {
	d := snapDTO{Found: s.Found, Value: s.Value, Confidence: round2(s.Confidence), Conflict: s.Conflict,
		WeakDisagreement: s.WeakDisagreement, Stale: s.Stale, Evidence: []evidenceSnapDTO{}}
	for _, e := range s.Evidence {
		d.Evidence = append(d.Evidence, evidenceSnapDTO{ID: e.ID, Source: e.Source, AuthorType: e.AuthorType, AuthorTrust: round2(e.AuthorTrust),
			Claimed: e.Claimed, Reliability: round2(e.Reliability), Contribution: round2(e.Contribution), ObservedAt: e.ObservedAt})
	}
	return d
}

func conflictChange(before, after string) string {
	switch {
	case before == "" && after == "":
		return conflictNone
	case before == "":
		return conflictOpened
	case after == "":
		return conflictClosed
	}
	return conflictRemains
}

func newChangeDTO(reg domain.Registry, c *hotelier.Change) *changeDTO {
	if c == nil {
		return nil
	}
	d := &changeDTO{EntityType: c.EntityType, EntityID: c.EntityID, Key: c.Key, Title: titleOf(reg, c.Key),
		Before: newSnapDTO(c.Before), After: newSnapDTO(c.After), ConflictChange: conflictChange(c.Before.Conflict, c.After.Conflict)}
	if ch, ok := reg.Get(c.Key); ok {
		d.ValueField = ch.FieldName()
		if c.EntityType == domain.EntityRoomCategory && ch.Category != nil {
			d.ValueField = ch.Category.ValueField
		}
	}
	return d
}

type hotelSummaryDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	District string `json:"district"`
	// Persona — имитированная персона отельера (honest | lazy | embellishing); пусто — живой отельер.
	Persona string `json:"persona,omitempty"`
}

func handleHotelierHotels(svc HotelierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hs, err := svc.Hotels(r.Context())
		if err != nil {
			hotelierError(w, "список отелей", err)
			return
		}
		out := struct {
			Hotels []hotelSummaryDTO `json:"hotels"`
		}{Hotels: []hotelSummaryDTO{}}
		for _, h := range hs {
			out.Hotels = append(out.Hotels, hotelSummaryDTO{ID: h.ID, Name: h.Name, District: h.District, Persona: h.Persona})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type missedDemandDTO struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Demand     int    `json:"demand"`
	Text       string `json:"text"`
	QuestionID int64  `json:"question_id,omitempty"`
}

func handleHotelierDemand(svc HotelierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("hotel_id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id отеля"})
			return
		}
		ms, err := svc.MissedDemand(r.Context(), id)
		if err != nil {
			hotelierError(w, "упущенный спрос", err)
			return
		}
		out := struct {
			HotelID int64             `json:"hotel_id"`
			Missed  []missedDemandDTO `json:"missed"`
		}{HotelID: id, Missed: []missedDemandDTO{}}
		for _, m := range ms {
			out.Missed = append(out.Missed, missedDemandDTO{Key: m.Key, Title: m.Title, Demand: m.Demand, Text: m.Text, QuestionID: m.QuestionID})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// factAtRequest — адрес факта «сущность + характеристика» и, для исправления, новое значение.
type factAtRequest struct {
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	Key        string         `json:"key"`
	Value      string         `json:"value"`
	Details    map[string]any `json:"details"`
}

// handleHotelierFactAt подтверждает или исправляет факт по адресу, который не меняется при пересборке фактов.
func handleHotelierFactAt(svc HotelierService, reg domain.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("hotel_id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный id отеля"})
			return
		}
		var req factAtRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || req.EntityType == "" || req.EntityID == "" || req.Key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается JSON {\"entity_type\": …, \"entity_id\": …, \"key\": …, \"value\": …}"})
			return
		}
		addr := hotelier.FactAddress{EntityType: req.EntityType, EntityID: req.EntityID, Key: req.Key}
		var out *hotelier.FactOutcome
		switch r.PathValue("action") {
		case "confirm":
			out, err = svc.ConfirmFactAt(r.Context(), id, addr)
		case "correct":
			if req.Value == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "для исправления нужно value"})
				return
			}
			out, err = svc.CorrectFactAt(r.Context(), id, addr, req.Value, req.Details)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "действие: confirm или correct"})
			return
		}
		if err != nil {
			hotelierError(w, "факт", err)
			return
		}
		dto := factOutcomeDTO{FactID: out.FactID, Key: out.Key, Action: out.Action, Claimed: out.Claimed,
			Evidence: out.Evidence, Recheck: out.Recheck, Note: out.Note, Change: newChangeDTO(reg, out.Change)}
		if t := out.Trust; t != nil {
			dto.Trust = &trustDTO{round2(t.Before), round2(t.After), t.Agree, t.Total}
		}
		writeJSON(w, http.StatusOK, dto)
	}
}

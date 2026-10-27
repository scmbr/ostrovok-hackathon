// Пакет admin собирает данные экрана «под капотом»: журнал прогонов агентов и конфликты со сторонами, порогом θ,
// связанным вопросом отельеру и задачей пересчёта. Только чтение.
package admin

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/explain"
)

// Run — прогон агента: записи журнала с одним session_id.
type Run struct {
	Agent     string
	SessionID string
	HotelID   int64 // 0 — прогон не про отель (диалог)
	Mode      string
	Steps     int
	// Refusals — отказы: попытки нарушить правила агента, они пишутся в журнал с решением «отказ: …».
	Refusals  int
	StartedAt time.Time
	EndedAt   time.Time
	Title     string // первое решение прогона
}

// Conflict — запись conflicts.
type Conflict struct {
	ID                         int64
	EntityType, EntityID, Key  string
	HotelID                    int64
	Status                     string
	Description                string
	Resolution, ResolutionKind string
	DetectedAt                 time.Time
}

// ConflictFilter — выбор конфликтов: открытые, закрытые (с причиной) или все; отель необязателен.
type ConflictFilter struct {
	Status  string // open | closed | all
	HotelID int64
	Limit   int
}

// QuestionRef — вопрос отельеру по той же сущности и характеристике.
type QuestionRef struct {
	ID     int64
	Status string
	Format string
}

// Task — задача пересчёта (recompute_tasks).
type Task struct {
	ID        int64
	HotelID   int64
	Key       string
	Reason    string
	Status    string
	Outcome   string
	CreatedAt time.Time
	DoneAt    *time.Time
}

// StaleFact — активный факт, поставленный на пересчёт.
type StaleFact struct {
	EntityType, EntityID, Key string
	HotelID                   int64
	ValidUntil                *time.Time
}

// Store — порт чтения. Реализация — adapter/repository/postgres.
type Store interface {
	Runs(ctx context.Context, agentName string, hotelID int64, limit int) ([]Run, error)
	RunSteps(ctx context.Context, sessionID string) ([]agent.TraceStep, error)
	Conflicts(ctx context.Context, f ConflictFilter) ([]Conflict, error)
	Question(ctx context.Context, hotelID int64, key, entityID string) (QuestionRef, bool, error)
	Tasks(ctx context.Context, hotelID int64, limit int) ([]Task, error)
	StaleFacts(ctx context.Context, hotelID int64, limit int) ([]StaleFact, error)
	Summary(ctx context.Context) (Summary, error)
}

// Coverage — покрытие характеристики: у скольких демо-отелей есть активный факт уровня отеля и какая у них уверенность.
type Coverage struct {
	Key           string
	Hotels        int
	AvgConfidence float64
	LowConfidence int // уверенность ниже порога «уверенного факта» (domain.ConfidentFact)
}

// Summary — счётчики из существующих данных; полные метрики качества — этап 8.
type Summary struct {
	DemoHotels    int
	Coverage      []Coverage
	OpenConflicts int
	WeakFacts     int
	StaleFacts    int
	// Questions — вопросы отельерам по статусам; HotelsAsked — демо-отелей, которым хотя бы раз задан вопрос.
	Questions   map[string]int
	HotelsAsked int
}

// Service — чтение для «под капотом».
type Service struct {
	Registry domain.Registry
	Store    Store
	Explain  *explain.Service
}

// SideEvidence — свидетельство на стороне спора.
type SideEvidence struct {
	AuthorType, AuthorID, Source string
	Contribution                 float64
	Comment                      string
	ZeroWeight                   bool
}

// Side — сторона спора: значение, вес (сумма логарифмов шансов) и свидетельства.
type Side struct {
	Value    string
	Weight   float64
	Evidence []SideEvidence
}

// ConflictView — конфликт с разбором. Стороны, θ и вес считаются движком сейчас (только у открытых); у закрытых есть
// причина закрытия.
type ConflictView struct {
	Conflict
	Title        string
	Sides        []Side
	Theta        float64
	ThetaLowered bool
	// ThetaReason — почему такой θ: обычный из реестра или пониженный, потому что в споре участвует отельер.
	ThetaReason string
	Question    *QuestionRef
}

// Conflicts возвращает конфликты с разбором по сторонам.
func (s *Service) Conflicts(ctx context.Context, f ConflictFilter) ([]ConflictView, error) {
	cs, err := s.Store.Conflicts(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]ConflictView, 0, len(cs))
	for _, c := range cs {
		v := ConflictView{Conflict: c, Title: c.Key}
		if ch, ok := s.Registry.Get(c.Key); ok {
			v.Title = ch.Title
		}
		if c.Status == domain.ConflictOpen && s.Explain != nil {
			fv, ok, err := s.Explain.Entity(ctx, c.EntityType, c.EntityID, c.Key)
			if err != nil {
				return nil, err
			}
			if ok {
				v.fill(fv.Explanation)
			}
		}
		if c.HotelID > 0 {
			entity := ""
			if c.EntityType == domain.EntityRoomCategory {
				entity = c.EntityID
			}
			q, ok, err := s.Store.Question(ctx, c.HotelID, c.Key, entity)
			if err != nil {
				return nil, err
			}
			if ok {
				v.Question = &q
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// fill раскладывает свидетельства по сторонам: вес стороны — сумма логарифмов шансов её свидетельств.
func (v *ConflictView) fill(x domain.FactExplanation) {
	v.Theta, v.ThetaLowered = x.Theta, x.ThetaLowered
	if x.ThetaLowered {
		v.ThetaReason = fmt.Sprintf("θ = %.1f понижен (conflict_theta_hotelier): в споре участвует утверждение отельера", x.Theta)
	} else {
		v.ThetaReason = fmt.Sprintf("θ = %.1f из реестра", x.Theta)
	}
	byValue := map[string]*Side{}
	var order []string
	for _, e := range x.Evidence {
		if e.Role == domain.RoleIgnoredD {
			continue
		}
		val := fmt.Sprint(e.Evidence.Claimed[x.ValueField])
		sd, ok := byValue[val]
		if !ok {
			sd = &Side{Value: val}
			byValue[val] = sd
			order = append(order, val)
		}
		sd.Weight += e.Logit
		comment, _ := e.Evidence.Details["comment"].(string)
		sd.Evidence = append(sd.Evidence, SideEvidence{AuthorType: e.Evidence.AuthorType, AuthorID: e.Evidence.AuthorID, Source: e.Evidence.Source,
			Contribution: e.Contribution, Comment: comment, ZeroWeight: e.ZeroWeight})
	}
	for _, val := range order {
		v.Sides = append(v.Sides, *byValue[val])
	}
	sort.SliceStable(v.Sides, func(a, b int) bool { return v.Sides[a].Weight > v.Sides[b].Weight })
}

// Runs — прогоны агентов, новые первыми; agentName и hotelID необязательны.
func (s *Service) Runs(ctx context.Context, agentName string, hotelID int64, limit int) ([]Run, error) {
	return s.Store.Runs(ctx, agentName, hotelID, limit)
}

// RunSteps — записи прогона по порядку.
func (s *Service) RunSteps(ctx context.Context, sessionID string) ([]agent.TraceStep, error) {
	return s.Store.RunSteps(ctx, sessionID)
}

// Tasks — задачи пересчёта.
func (s *Service) Tasks(ctx context.Context, hotelID int64, limit int) ([]Task, error) {
	return s.Store.Tasks(ctx, hotelID, limit)
}

// StaleFacts — факты, поставленные на пересчёт.
func (s *Service) StaleFacts(ctx context.Context, hotelID int64, limit int) ([]StaleFact, error) {
	return s.Store.StaleFacts(ctx, hotelID, limit)
}

// Summary — счётчики для сводки «под капотом».
func (s *Service) Summary(ctx context.Context) (Summary, error) { return s.Store.Summary(ctx) }

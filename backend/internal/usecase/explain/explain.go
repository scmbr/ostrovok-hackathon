// Пакет explain раскладывает факты на свидетельства для «под капотом»: цепочка надёжности, вклады, судья, итог.
// Только чтение: считает ту же функцию движка правды (domain.ExplainFact), что и сборка факта.
package explain

import (
	"context"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Store — порт чтения свидетельств. Реализация — adapter/repository/postgres.
type Store interface {
	// HotelEvidence — активные свидетельства отеля, его фасадов и категорий номеров (без размещений номеров).
	HotelEvidence(ctx context.Context, hotelID int64) ([]domain.Evidence, error)
	// EntityEvidence — активные свидетельства сущности по характеристике.
	EntityEvidence(ctx context.Context, entityType, entityID, key string) ([]domain.Evidence, error)
	// Trusts — репутация авторов-людей.
	Trusts(ctx context.Context) (map[domain.Author]float64, error)
}

// Service раскладывает факты.
type Service struct {
	Registry domain.Registry
	Store    Store
	Now      func() time.Time
}

// FactView — разобранный факт с названием характеристики.
type FactView struct {
	EntityType, EntityID, Key, Title string
	Explanation                      domain.FactExplanation
}

func (s *Service) ctx(ctx context.Context) (domain.TruthContext, error) {
	trust, err := s.Store.Trusts(ctx)
	if err != nil {
		return domain.TruthContext{}, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return domain.TruthContext{Now: now, Trust: trust, Rules: s.Registry.Rules()}, nil
}

func (s *Service) title(key string) string {
	if c, ok := s.Registry.Get(key); ok {
		return c.Title
	}
	return key
}

// Hotel раскладывает все факты отеля, его фасадов и категорий номеров.
func (s *Service) Hotel(ctx context.Context, hotelID int64) ([]FactView, error) {
	evs, err := s.Store.HotelEvidence(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	tc, err := s.ctx(ctx)
	if err != nil {
		return nil, err
	}
	type k struct{ t, id, key string }
	groups := map[k][]domain.Evidence{}
	var order []k
	for _, e := range evs {
		g := k{e.EntityType, e.EntityID, e.Key}
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], e)
	}
	var out []FactView
	for _, g := range order {
		x, ok := domain.ExplainFact(groups[g], tc)
		if !ok {
			continue
		}
		out = append(out, FactView{EntityType: g.t, EntityID: g.id, Key: g.key, Title: s.title(g.key), Explanation: x})
	}
	return out, nil
}

// Entity раскладывает факт одной сущности и характеристики; ok=false, если свидетельств нет.
func (s *Service) Entity(ctx context.Context, entityType, entityID, key string) (FactView, bool, error) {
	evs, err := s.Store.EntityEvidence(ctx, entityType, entityID, key)
	if err != nil || len(evs) == 0 {
		return FactView{}, false, err
	}
	tc, err := s.ctx(ctx)
	if err != nil {
		return FactView{}, false, err
	}
	x, ok := domain.ExplainFact(evs, tc)
	return FactView{EntityType: entityType, EntityID: entityID, Key: key, Title: s.title(key), Explanation: x}, ok, nil
}

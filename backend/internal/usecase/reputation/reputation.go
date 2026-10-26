// Пакет reputation пересчитывает репутацию авторов-людей (author_trust): доля утверждений автора, подтверждённых
// независимым надёжным свидетельством. Пересчёт идёт заново по всем активным свидетельствам автора, а не инкрементом.
package reputation

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Claim — утверждение автора и свидетельства других авторов о той же сущности и характеристике.
type Claim struct {
	Evidence domain.Evidence
	Others   []domain.Evidence
}

// Store — порт хранилища репутации.
type Store interface {
	// AuthorClaims — активные свидетельства автора с независимыми свидетельствами по тем же сущностям.
	AuthorClaims(ctx context.Context, a domain.Author) ([]Claim, error)
	// Trust — текущая репутация; found=false, если записи нет.
	Trust(ctx context.Context, a domain.Author) (agree, total int, trust float64, found bool, err error)
	SaveTrust(ctx context.Context, a domain.Author, agree, total int, trust float64) error
	// RefreshFacts пересобирает факты, в которые входят свидетельства автора: его вес изменился.
	RefreshFacts(ctx context.Context, a domain.Author) error
}

// Service пересчитывает репутацию.
type Service struct {
	Registry domain.Registry
	Store    Store
	Now      func() time.Time
}

// Judged — одно сравнённое утверждение, для журнала и отчёта.
type Judged struct {
	EntityType, EntityID, Key string
	Claimed                   map[string]any
	Verdict                   domain.Verdict
}

// Update — итог пересчёта.
type Update struct {
	Author        domain.Author
	Before, After float64
	Agree, Total  int
	Judged        []Judged
}

// Update пересчитывает репутацию автора a по всем его активным свидетельствам, для которых есть независимое
// свидетельство с r ≥ domain.MinIndependentReliability, и, если вес автора изменился, пересобирает его факты.
// Название «Sea View» из мока учитывается как обычное утверждение отельера.
func (s *Service) Update(ctx context.Context, a domain.Author) (Update, error) {
	u := Update{Author: a}
	_, _, before, found, err := s.Store.Trust(ctx, a)
	if err != nil {
		return u, fmt.Errorf("репутация: %w", err)
	}
	if !found {
		before = domain.DefaultTrust
	}
	claims, err := s.Store.AuthorClaims(ctx, a)
	if err != nil {
		return u, fmt.Errorf("утверждения автора: %w", err)
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	tc := domain.TruthContext{Now: now, Rules: s.Registry.Rules()}
	for _, cl := range claims {
		v := domain.JudgeClaim(cl.Evidence, cl.Others, tc)
		if !v.Counted {
			continue
		}
		u.Total++
		if v.Agree {
			u.Agree++
		}
		u.Judged = append(u.Judged, Judged{cl.Evidence.EntityType, cl.Evidence.EntityID, cl.Evidence.Key, cl.Evidence.Claimed, v})
	}
	u.Before, u.After = before, domain.TrustFromCounts(u.Agree, u.Total)
	if u.Total == 0 && !found {
		u.After = before
		return u, nil // сравнивать нечего, записи нет: новичок остаётся новичком
	}
	if err := s.Store.SaveTrust(ctx, a, u.Agree, u.Total, u.After); err != nil {
		return u, fmt.Errorf("запись репутации: %w", err)
	}
	if math.Abs(u.After-u.Before) > 1e-9 {
		if err := s.Store.RefreshFacts(ctx, a); err != nil {
			return u, fmt.Errorf("пересборка фактов: %w", err)
		}
	}
	return u, nil
}

package rooms

import (
	"context"
	"math"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// probabilityShift — сдвиг вероятности искомого значения в категории, который считается изменением.
const probabilityShift = 0.01

// uncertain — факт фасада, который ответ отельера может перевернуть: уверенность ниже отметки «свежо»,
// конфликт, слабое расхождение источников или устаревшее свидетельство.
func uncertain(f domain.Fact) bool {
	return f.Confidence < domain.FreshConfidenceMark || f.Conflict != nil || f.WeakDisagreement || f.Stale
}

// CategoryImpact считает, у скольких категорий отеля ответ отельера изменит вероятность искомого значения
// характеристики key: у неуверенных фасадов значение переворачивается (искомое ↔ нет) и сравнивается вероятность
// AssessCategory до и после. changed — число таких категорий, total — всего категорий с размещением.
// У характеристик без правила category total = 0: категориям нечего менять.
func (s *Service) CategoryImpact(ctx context.Context, hotelID int64, key string) (changed, total int, err error) {
	var pr *projection
	facts, err := s.Store.FacadeFacts(ctx, hotelID, []string{key})
	if err != nil {
		return 0, 0, err
	}
	for _, p := range projections(s.Registry, facts) {
		if p.char.Key == key {
			p := p
			pr = &p
		}
	}
	if pr == nil {
		return 0, 0, nil
	}
	cats, err := s.Store.Categories(ctx, hotelID)
	if err != nil {
		return 0, 0, err
	}
	flipped := func(facadeID int64, floor int) (string, bool) {
		v, ok := pr.valueAt(facadeID, floor)
		if f, has := pr.facts[facadeID]; !ok || !has || !uncertain(f) {
			return v, ok
		}
		if v == pr.rule.Target {
			return pr.rule.Values[len(pr.rule.Values)-1], true
		}
		return pr.rule.Target, true
	}
	for _, c := range cats {
		if len(c.Placements) == 0 {
			continue
		}
		total++
		base := domain.AssessCategory(c.Rooms, c.Placed(), pr.valueAt, pr.rule.Target)
		alt := domain.AssessCategory(c.Rooms, c.Placed(), flipped, pr.rule.Target)
		if math.Abs(base.Probability-alt.Probability) > probabilityShift {
			changed++
		}
	}
	return changed, total, nil
}

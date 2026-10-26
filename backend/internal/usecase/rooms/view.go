package rooms

import (
	"context"
	"fmt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Service отдаёт категории отеля с вероятностями и гарантиями по характеристикам.
type Service struct {
	Registry domain.Registry
	Store    Store
}

// CharacteristicView — характеристика в категории: расчёт по текущим фактам фасадов и итоговый факт категории.
type CharacteristicView struct {
	Key        string
	Title      string
	Assessment domain.CategoryAssessment
	// Fact — итог движка правды по категории (с утверждением названия, если оно было); nil — факта нет.
	Fact *CategoryFact
}

// CategoryView — категория номеров с размещением и характеристиками.
type CategoryView struct {
	Category        domain.RoomCategory
	Characteristics []CharacteristicView
}

// HotelRooms — категории отеля и его фасады.
type HotelRooms struct {
	HotelID    int64
	Facades    []FacadeInfo
	Categories []CategoryView

	prs map[string]projection
}

// belowFloor — псевдозначение номеров ниже запрошенного этажа.
const belowFloor = "below_floor"

// AssessAt считает категорию по характеристике key для искомого значения target (пусто — цель из реестра) и номеров не ниже
// minFloor (0 — любой этаж). Номера делятся по этажам поровну, как в AssessCategory.
func (h HotelRooms) AssessAt(categoryID int64, key, target string, minFloor int) (domain.CategoryAssessment, bool) {
	pr, ok := h.prs[key]
	if !ok {
		return domain.CategoryAssessment{}, false
	}
	for _, c := range h.Categories {
		if c.Category.ID != categoryID {
			continue
		}
		if target == "" {
			target = pr.rule.Target
		}
		at := pr.valueAt
		if minFloor > 0 {
			// Номера ниже нужного этажа не подходят ни под какое значение: их доля уходит в «не подходит», пожелание строится выше.
			at = func(facadeID int64, floor int) (string, bool) {
				if floor < minFloor {
					return belowFloor, true
				}
				return pr.valueAt(facadeID, floor)
			}
		}
		return domain.AssessCategory(c.Category.Rooms, c.Category.Placed(), at, target), true
	}
	return domain.CategoryAssessment{}, false
}

// Hotel собирает категории отеля. Вероятность и пожелание считаются по текущим фактам фасадов,
// значение, уверенность и конфликт — из фактов категории (движок правды).
func (s *Service) Hotel(ctx context.Context, hotelID int64) (HotelRooms, error) {
	out := HotelRooms{HotelID: hotelID}
	cats, err := s.Store.Categories(ctx, hotelID)
	if err != nil {
		return out, err
	}
	if len(cats) == 0 {
		return out, ErrNotFound
	}
	if out.Facades, err = s.Store.Facades(ctx, hotelID); err != nil {
		return out, err
	}
	facts, err := s.Store.FacadeFacts(ctx, hotelID, projectionKeys(s.Registry))
	if err != nil {
		return out, fmt.Errorf("факты фасадов: %w", err)
	}
	catFacts, err := s.Store.CategoryFacts(ctx, hotelID)
	if err != nil {
		return out, fmt.Errorf("факты категорий: %w", err)
	}
	prs := projections(s.Registry, facts)
	out.prs = map[string]projection{}
	for _, pr := range prs {
		out.prs[pr.char.Key] = pr
	}
	for _, c := range cats {
		v := CategoryView{Category: c}
		for _, pr := range prs {
			cv := CharacteristicView{Key: pr.char.Key, Title: pr.char.Title, Assessment: pr.assess(c)}
			for i := range catFacts {
				if catFacts[i].CategoryID == c.ID && catFacts[i].Fact.Key == pr.char.Key {
					cv.Fact = &catFacts[i]
				}
			}
			v.Characteristics = append(v.Characteristics, cv)
		}
		out.Categories = append(out.Categories, v)
	}
	return out, nil
}

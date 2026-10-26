package rooms

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Projector строит расчётные свидетельства о категориях: значение характеристики в категории из фактов
// фасадов и размещения номеров. Свидетельство участвует в движке правды наравне с утверждениями отельера
// (название «Sea View» — утверждение отеля, расчёт его проверяет).
type Projector struct {
	Registry domain.Registry
	Store    Store
	Now      func() time.Time // для тестов; по умолчанию time.Now
}

// Result — итог прогона.
type Result struct {
	Hotels     int
	Categories int
	Evidence   int // записано свидетельств
}

// Run пересчитывает свидетельства категорий у отелей (по flt) со всеми характеристиками, у которых есть
// правило category. Прежние расчётные свидетельства категории снимаются; ответы отельера остаются.
func (p *Projector) Run(ctx context.Context, flt domain.HotelFilter) (Result, error) {
	var res Result
	hotels, err := p.Store.HotelsWithCategories(ctx, flt)
	if err != nil {
		return res, fmt.Errorf("отели с категориями: %w", err)
	}
	if len(hotels) == 0 && flt.HotelID != 0 {
		return res, fmt.Errorf("у отеля %d нет размещённых категорий (collect mock-rooms)", flt.HotelID)
	}
	keys := projectionKeys(p.Registry)
	for i, id := range hotels {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		cats, err := p.Store.Categories(ctx, id)
		if err != nil {
			return res, fmt.Errorf("отель %d: %w", id, err)
		}
		facts, err := p.Store.FacadeFacts(ctx, id, keys)
		if err != nil {
			return res, fmt.Errorf("отель %d: %w", id, err)
		}
		for _, pr := range projections(p.Registry, facts) {
			for _, c := range cats {
				ev, ok := p.evidence(pr, c)
				var evs []domain.Evidence
				if ok {
					evs = append(evs, ev)
					res.Evidence++
				}
				author := domain.ToolAuthor(domain.RoomProjectionMethod)
				if err := p.Store.SaveCategoryEvidence(ctx, c.ID, pr.char.Key, []domain.Author{author}, evs); err != nil {
					return res, fmt.Errorf("отель %d, категория %q, %s: %w", id, c.Name, pr.char.Key, err)
				}
			}
		}
		res.Hotels++
		res.Categories += len(cats)
		if (i+1)%25 == 0 {
			log.Printf("rooms: %d из %d отелей", i+1, len(hotels))
		}
	}
	return res, nil
}

// evidence строит расчётное свидетельство категории. Значение — преобладающее среди номеров (при равенстве —
// первое в values), надёжность — средняя уверенность фактов фасадов, умноженная на долю номеров с этим
// значением: чем «смешаннее» категория, тем слабее утверждение. Нет фактов фасадов — свидетельства нет.
func (p *Projector) evidence(pr projection, c domain.RoomCategory) (domain.Evidence, bool) {
	conf, ok := pr.confidence(c)
	if !ok {
		return domain.Evidence{}, false
	}
	a := pr.assess(c)
	value, share := dominant(pr.rule.Values, a.Shares)
	if share == 0 {
		return domain.Evidence{}, false
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	until := now.Add(pr.char.TTL())

	shares := map[string]any{}
	for _, v := range pr.rule.Values {
		shares[v] = round(a.Shares[v])
	}
	claimed := map[string]any{
		pr.rule.ValueField: value,
		"target":           pr.rule.Target,
		"probability":      round(a.Probability),
		"guarantee":        a.Guarantee,
		"shares":           shares,
		"unknown":          round(a.Unknown),
		"rooms":            c.Rooms,
	}
	if a.Wish != nil {
		claimed["wish"] = wishMap(*a.Wish)
	}
	placements := make([]map[string]any, len(c.Placements))
	factIDs := map[string]any{}
	for i, pl := range c.Placements {
		placements[i] = map[string]any{
			"facade_id": pl.FacadeID, "floor_from": pl.FloorFrom, "floor_to": pl.FloorTo, "rooms_count": pl.Rooms,
		}
		if f, ok := pr.facts[pl.FacadeID]; ok {
			factIDs[strconv.FormatInt(pl.FacadeID, 10)] = f.ID
		}
	}
	return domain.Evidence{
		EntityType: domain.EntityRoomCategory, EntityID: strconv.FormatInt(c.ID, 10), Key: pr.char.Key, Claimed: claimed,
		Source: domain.SourceComputed, Method: domain.RoomProjectionMethod,
		AuthorType: domain.AuthorTool, AuthorID: domain.RoomProjectionMethod,
		Reliability: math.Min(math.Max(conf*share, domain.MinReliability), domain.MaxReliability),
		ObservedAt:  now, ValidUntil: &until,
		Details: map[string]any{
			"placements":      placements,
			"facade_fact_ids": factIDs,
			"assumptions": []string{
				"номера размещения делятся по этажам диапазона поровну",
				"надёжность = средняя уверенность фактов фасадов × доля номеров с преобладающим значением",
			},
		},
	}, true
}

// dominant — значение с наибольшей долей; при равенстве — раньше в values.
func dominant(values []string, shares map[string]float64) (string, float64) {
	best, bestShare := "", 0.0
	for _, v := range values {
		if shares[v] > bestShare+1e-9 {
			best, bestShare = v, shares[v]
		}
	}
	return best, bestShare
}

func wishMap(w domain.Wish) map[string]any {
	return map[string]any{"facade_id": w.FacadeID, "min_floor": w.MinFloor, "rooms": round(w.Rooms), "share": round(w.Share)}
}

func round(x float64) float64 { return math.Round(x*100) / 100 }

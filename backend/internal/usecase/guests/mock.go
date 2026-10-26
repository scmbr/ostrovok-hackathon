package guests

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Мок заселений: у каждого демо-отеля две брони с пожеланием, гость — честный (70 %) или ленивый. У одного отеля
// вместо этого три гостя closed_view: в честной категории «Sea View» (расчёт гарантирует вид) они сообщают, что вид
// закрыт. Всё детерминировано от id отеля; в README помечено как мок.

// Доли мока.
const (
	honestShare       = 0.7
	staysPerHotel     = 2
	closedViewGuests  = domain.SignalMinGuests
	minClosedViewRoom = closedViewGuests
)

// StayResult — итог мока заселений.
type StayResult struct {
	Hotels          int
	Stays           int
	ByPersona       map[string]int
	ClosedViewHotel *Hotel
	ClosedViewCat   string
}

// Stayer создаёт имитированных гостей и брони.
type Stayer struct {
	Registry domain.Registry
	Store    Store
	Rooms    Rooms
}

// cell — ячейка «фасад × этаж» внутри категории.
type cell struct {
	facade int64
	floor  int
}

func categoryCells(cat domain.RoomCategory) []cell {
	var out []cell
	for _, p := range cat.Placements {
		for f := p.FloorFrom; f <= p.FloorTo; f++ {
			out = append(out, cell{p.FacadeID, f})
		}
	}
	return out
}

// Run пересоздаёт гостей и брони демо-отелей (прежние гости, анкеты, их свидетельства и репутация удаляются).
func (m *Stayer) Run(ctx context.Context, flt domain.HotelFilter) (StayResult, error) {
	res := StayResult{ByPersona: map[string]int{}}
	// Сброс идёт первым: конфликты от прежних ответов гостей не должны попасть в список приукрашивающих отелей.
	if err := m.Store.ResetGuests(ctx); err != nil {
		return res, fmt.Errorf("сброс гостей: %w", err)
	}
	hotels, err := m.Store.DemoHotels(ctx, flt)
	if err != nil {
		return res, fmt.Errorf("демо-отели: %w", err)
	}
	if len(hotels) == 0 {
		return res, fmt.Errorf("нет демо-отелей (collect select-demo)")
	}
	var keys []string
	for _, c := range m.Registry.All() {
		if c.Category != nil {
			keys = append(keys, c.Key)
		}
	}
	var stays []Stay
	closedDone := false
	for _, h := range hotels {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if h.Embellishing {
			continue
		}
		cells, err := m.Rooms.Cells(ctx, h.ID)
		if err != nil {
			continue // у отеля нет размещённых категорий
		}
		cats := cells.Categories()
		if len(cats) == 0 {
			continue
		}
		res.Hotels++
		if !closedDone {
			if cat, ok := honestSeaViewCategory(cells, cats); ok {
				stays = append(stays, closedViewStays(h, cat)...)
				closedDone = true
				hh := h
				res.ClosedViewHotel, res.ClosedViewCat = &hh, cat.Name
				continue
			}
		}
		rng := rand.New(rand.NewSource(h.ID*104729 + 7))
		order := rng.Perm(len(cats))
		for i := 0; i < len(order) && i < staysPerHotel; i++ {
			cat := cats[order[i]]
			cs := categoryCells(cat)
			if len(cs) == 0 {
				continue
			}
			persona := domain.GuestHonest
			if rng.Float64() >= honestShare {
				persona = domain.GuestLazy
			}
			wishes := map[string]any{}
			pick := cs[rng.Intn(len(cs))]
			if a, ok := cells.Assess(domain.SeaViewKey, cat.ID); ok && a.Wish != nil {
				// Бронь с пожеланием «фасад, этаж N+»: отель его выполнил, гостя поселили в этой части категории.
				w := a.Wish
				var fit []cell
				for _, c := range cs {
					if c.facade == w.FacadeID && c.floor >= w.MinFloor {
						fit = append(fit, c)
					}
				}
				if len(fit) > 0 {
					pick = fit[rng.Intn(len(fit))]
					wishes[domain.SeaViewKey] = map[string]any{"facade_id": w.FacadeID, "min_floor": w.MinFloor}
				}
			}
			stays = append(stays, Stay{
				GuestName: fmt.Sprintf("Гость %d-%d", h.ID, i+1), Persona: persona, HotelID: h.ID, CategoryID: cat.ID,
				FacadeID: pick.facade, Floor: pick.floor, Wishes: wishes, Expectations: expectationsAt(cells, keys, pick),
			})
		}
	}
	if !closedDone {
		return res, fmt.Errorf("не нашёл демо-отель с честной категорией «Sea View» для гостей closed_view (collect mock-rooms)")
	}
	if err := m.Store.SaveStays(ctx, stays); err != nil {
		return res, err
	}
	for _, s := range stays {
		res.ByPersona[s.Persona]++
	}
	res.Stays = len(stays)
	return res, nil
}

// honestSeaViewCategory — первая категория с названием «Sea View», где расчёт гарантирует вид: достаточно номеров
// и не меньше трёх разных ячеек «фасад × этаж» для трёх гостей.
func honestSeaViewCategory(cells interface {
	Assess(key string, categoryID int64) (domain.CategoryAssessment, bool)
}, cats []domain.RoomCategory) (domain.RoomCategory, bool) {
	for _, c := range cats {
		a, ok := cells.Assess(domain.SeaViewKey, c.ID)
		if ok && domain.TitleClaimsSeaView(c.Name) && a.Guarantee && c.Rooms >= minClosedViewRoom && len(categoryCells(c)) >= closedViewGuests {
			return c, true
		}
	}
	return domain.RoomCategory{}, false
}

// closedViewStays — три гостя closed_view в разных ячейках категории (с нижнего, среднего и верхнего этажа).
func closedViewStays(h Hotel, cat domain.RoomCategory) []Stay {
	cs := categoryCells(cat)
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].floor != cs[j].floor {
			return cs[i].floor < cs[j].floor
		}
		return cs[i].facade < cs[j].facade
	})
	idx := []int{0, len(cs) / 2, len(cs) - 1}
	var out []Stay
	for i, k := range idx {
		c := cs[k]
		out = append(out, Stay{
			GuestName: fmt.Sprintf("Гость %d-закрытый вид %d", h.ID, i+1), Persona: domain.GuestClosedView,
			HotelID: h.ID, CategoryID: cat.ID, FacadeID: c.facade, Floor: c.floor,
			Wishes:       map[string]any{domain.SeaViewKey: map[string]any{"facade_id": c.facade, "min_floor": 0}},
			Expectations: []string{domain.SeaViewKey},
		})
	}
	return out
}

// expectationsAt — характеристики, по которым у ячейки есть расчёт: их гость ожидал в брони.
func expectationsAt(cells CellSource, keys []string, c cell) []string {
	var out []string
	for _, k := range keys {
		if _, ok := cells.ValueAt(k, c.facade, c.floor); ok {
			out = append(out, k)
		}
	}
	return out
}

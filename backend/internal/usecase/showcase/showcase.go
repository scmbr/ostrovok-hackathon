// Package showcase — витрина демо-отелей для главной страницы: карточка и 1–2 проверенных факта.
package showcase

import (
	"context"
	"sort"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// MaxBeachMinutes — дальше этого пешком до пляжа бейдж не показываем.
const MaxBeachMinutes = 15

// Raw — отель из хранилища: сырые значения фактов уровня отеля, nil — факта нет.
type Raw struct {
	ID       int64
	Name     string
	District string
	Card     domain.Card
	// SeaMinFloor — min_floor факта sea_view: не nil, если по расчёту море видно с какого-то этажа.
	SeaMinFloor *int
	// BeachMinutes — minutes факта walk_to_beach.
	BeachMinutes *float64
}

// Store — порт хранилища.
type Store interface {
	DemoHotelsRaw(ctx context.Context, regionID int64) ([]Raw, error)
}

// Hotel — отель витрины. Факты показываются, только если они что-то обещают гостю.
type Hotel struct {
	ID       int64
	Name     string
	District string
	Card     domain.Card
	// SeaView — море видно из окон хотя бы одного фасада.
	SeaView bool
	// BeachMin — минут пешком до пляжа, 0 — не показывать (дальше MaxBeachMinutes или неизвестно).
	BeachMin int
}

type Service struct {
	Store    Store
	RegionID int64
}

// Badges переводит сырые значения фактов в показываемые: вид — если он есть, пляж — если не дальше порога.
func Badges(r Raw) (seaView bool, beachMin int) {
	seaView = r.SeaMinFloor != nil
	if r.BeachMinutes != nil && *r.BeachMinutes >= 0 && *r.BeachMinutes <= MaxBeachMinutes {
		beachMin = max(1, int(*r.BeachMinutes+0.5))
	}
	return seaView, beachMin
}

// List возвращает демо-отели: сначала с видом на море, затем с пляжем поблизости (ближе — раньше), затем по id.
func (s *Service) List(ctx context.Context) ([]Hotel, error) {
	raw, err := s.Store.DemoHotelsRaw(ctx, s.RegionID)
	if err != nil {
		return nil, err
	}
	out := make([]Hotel, 0, len(raw))
	for _, r := range raw {
		sea, beach := Badges(r)
		out = append(out, Hotel{ID: r.ID, Name: r.Name, District: r.District, Card: r.Card, SeaView: sea, BeachMin: beach})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SeaView != b.SeaView {
			return a.SeaView
		}
		if (a.BeachMin > 0) != (b.BeachMin > 0) {
			return a.BeachMin > 0
		}
		if a.BeachMin != b.BeachMin {
			return a.BeachMin < b.BeachMin
		}
		return a.ID < b.ID
	})
	return out, nil
}

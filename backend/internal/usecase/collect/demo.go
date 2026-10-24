package collect

import (
	"context"
	"fmt"
	"sort"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// DemoSize — сколько отелей попадает в демо (PROJECT_CONTEXT.md: 40–60).
const DemoSize = 50

// alwaysDemoDistrict — район, где отелей мало, а нужен вид на море: берём все.
const alwaysDemoDistrict = "barceloneta"

// SelectDemoIDs выбирает до n отелей: все из alwaysDemoDistrict, остальное место
// делится поровну между районами по кругу. Внутри района раньше идут отели со зданием
// и известной высотой, затем просто со зданием, затем остальные; при равенстве — по id.
func SelectDemoIDs(cands []domain.DemoCandidate, n int) []int64 {
	byDistrict := map[string][]domain.DemoCandidate{}
	for _, c := range cands {
		byDistrict[c.District] = append(byDistrict[c.District], c)
	}
	for _, list := range byDistrict {
		sort.Slice(list, func(i, j int) bool {
			if ri, rj := rank(list[i]), rank(list[j]); ri != rj {
				return ri < rj
			}
			return list[i].ID < list[j].ID
		})
	}

	var ids []int64
	for _, c := range byDistrict[alwaysDemoDistrict] {
		if len(ids) == n {
			break
		}
		ids = append(ids, c.ID)
	}
	delete(byDistrict, alwaysDemoDistrict)

	names := make([]string, 0, len(byDistrict))
	for name := range byDistrict {
		names = append(names, name)
	}
	sort.Strings(names)

	for progress := true; progress && len(ids) < n; {
		progress = false
		for _, name := range names {
			if len(ids) == n {
				break
			}
			if list := byDistrict[name]; len(list) > 0 {
				ids = append(ids, list[0].ID)
				byDistrict[name] = list[1:]
				progress = true
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func rank(c domain.DemoCandidate) int {
	switch {
	case c.KnownHeight:
		return 0
	case c.HasBuilding:
		return 1
	default:
		return 2
	}
}

// SelectDemo отмечает в БД демо-подмножество и возвращает его размер.
func (c *Collector) SelectDemo(ctx context.Context, n int) (int, error) {
	cands, err := c.Store.ListDemoCandidates(ctx)
	if err != nil {
		return 0, err
	}
	ids := SelectDemoIDs(cands, n)
	if err := c.Store.SetDemo(ctx, ids); err != nil {
		return 0, fmt.Errorf("отметка демо-отелей: %w", err)
	}
	return len(ids), nil
}

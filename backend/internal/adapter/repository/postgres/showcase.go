package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/showcase"
)

// DemoHotelsRaw читает демо-отели региона с активными фактами уровня отеля sea_view и walk_to_beach.
func (s *SearchStore) DemoHotelsRaw(ctx context.Context, regionID int64) ([]showcase.Raw, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, h.name, COALESCE(h.district, ''), h.card,
  (SELECT f.value FROM facts f WHERE f.status = $2 AND f.entity_type = 'hotel' AND f.entity_id = h.id::text
     AND f.characteristic_key = $3 ORDER BY f.id DESC LIMIT 1),
  (SELECT f.value FROM facts f WHERE f.status = $2 AND f.entity_type = 'hotel' AND f.entity_id = h.id::text
     AND f.characteristic_key = $4 ORDER BY f.id DESC LIMIT 1)
FROM hotels h WHERE h.region_id = $1 AND h.demo ORDER BY h.id`,
		regionID, domain.FactActive, domain.SeaViewKey, domain.WalkToBeachKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []showcase.Raw
	for rows.Next() {
		var r showcase.Raw
		var card, sea, beach []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.District, &card, &sea, &beach); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(card, &r.Card); err != nil {
			return nil, fmt.Errorf("карточка отеля %d: %w", r.ID, err)
		}
		var sv struct {
			MinFloor *float64 `json:"min_floor"`
		}
		if sea != nil && json.Unmarshal(sea, &sv) == nil && sv.MinFloor != nil {
			f := int(*sv.MinFloor)
			r.SeaMinFloor = &f
		}
		var wb struct {
			Minutes *float64 `json:"minutes"`
		}
		if beach != nil && json.Unmarshal(beach, &wb) == nil {
			r.BeachMinutes = wb.Minutes
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

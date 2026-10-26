package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/collect"
)

// HotelierMockStore хранит мок отельера: персоны, спрос на характеристики и скрытую истину.
type HotelierMockStore struct {
	pool *pgxpool.Pool
}

func NewHotelierMockStore(pool *pgxpool.Pool) *HotelierMockStore {
	return &HotelierMockStore{pool: pool}
}

func (s *HotelierMockStore) DemoHotels(ctx context.Context, flt domain.HotelFilter) ([]collect.HotelRef, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, name FROM hotels
WHERE demo AND ($1::bigint = 0 OR region_id = $1) AND ($2::bigint = 0 OR id = $2)
ORDER BY id`, flt.RegionID, flt.HotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collect.HotelRef
	for rows.Next() {
		var h collect.HotelRef
		if err := rows.Scan(&h.ID, &h.Name); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// EmbellishingHotels — демо-отели с открытым конфликтом sea_view у категории номеров (те же, что в collect stats).
func (s *HotelierMockStore) EmbellishingHotels(ctx context.Context, regionID int64) (map[int64]bool, error) {
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT h.id
FROM conflicts k
JOIN room_categories c ON k.entity_type = $1 AND k.entity_id = c.id::text
JOIN hotels h ON h.id = c.hotel_id
WHERE k.characteristic_key = $2 AND k.status = $3 AND h.demo AND ($4::bigint = 0 OR h.region_id = $4)`,
		domain.EntityRoomCategory, domain.SeaViewKey, domain.ConflictOpen, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SaveHotelierMock заменяет мок в одной транзакции: персоны демо-отелей, истину по ним и спрос региона.
func (s *HotelierMockStore) SaveHotelierMock(ctx context.Context, regionID int64, personas map[int64]string, truth []collect.TruthRow, demand map[string]int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for id, p := range personas {
		if _, err := tx.Exec(ctx, `UPDATE hotels SET hotelier_persona = $2 WHERE id = $1`, id, p); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM mock_hotel_truth WHERE hotel_id = $1`, id); err != nil {
			return err
		}
	}
	for _, t := range truth {
		v, err := json.Marshal(t.Value)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mock_hotel_truth (hotel_id, characteristic_key, value) VALUES ($1, $2, $3)`, t.HotelID, t.Key, v); err != nil {
			return err
		}
	}
	for key, n := range demand {
		if _, err := tx.Exec(ctx, `
INSERT INTO characteristic_demand (characteristic_key, region_id, count, source) VALUES ($1, $2, $3, $4)
ON CONFLICT (characteristic_key, region_id) DO UPDATE SET count = EXCLUDED.count, source = EXCLUDED.source`,
			key, regionID, n, collect.DemandSourceMock); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

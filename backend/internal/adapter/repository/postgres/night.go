package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// NightStore — заведения рядом с отелями для night_services.
type NightStore struct {
	pool *pgxpool.Pool
	evidenceWriter
}

func NewNightStore(pool *pgxpool.Pool) *NightStore { return &NightStore{pool: pool, evidenceWriter: evidenceWriter{pool}} }

// NightTargets возвращает все отели с заведениями в радиусе NightRadiusM (расстояние по прямой, м) и их opening_hours.
// Отели без заведений тоже возвращаются: «ничего рядом» — тоже результат.
func (s *NightStore) NightTargets(ctx context.Context) ([]domain.NightTarget, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, COALESCE(p.osm_id, 0), COALESCE(p.kind, ''), COALESCE(p.name, ''),
       COALESCE(ST_Distance(h.geom::geography, p.geom::geography), 0), COALESCE(p.tags->>'opening_hours', '')
FROM hotels h
LEFT JOIN pois p ON p.geom && ST_Expand(h.geom, 0.005)
                AND ST_DWithin(h.geom::geography, p.geom::geography, $1::float8)
ORDER BY h.id, p.id`, domain.NightRadiusM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NightTarget
	for rows.Next() {
		var hotelID int64
		var p domain.NightPoi
		if err := rows.Scan(&hotelID, &p.OSMID, &p.Kind, &p.Name, &p.DistM, &p.Hours); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].HotelID != hotelID {
			out = append(out, domain.NightTarget{HotelID: hotelID})
		}
		if p.Kind != "" {
			t := &out[len(out)-1]
			t.Pois = append(t.Pois, p)
		}
	}
	return out, rows.Err()
}


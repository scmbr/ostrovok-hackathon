package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// NearestStreet возвращает название ближайшей именованной улицы не дальше radiusM от отеля или пустую строку.
// Магистрали, скоростные дороги и съезды (motorway, trunk, *_link) не берутся: «адрес» на съезде вводит в заблуждение.
// Выборка идёт по GiST-индексу roads_geom_idx: рамка по градусам отсекает лишнее, ST_DWithin по geography даёт метры.
func (s *SearchStore) NearestStreet(ctx context.Context, hotelID int64, radiusM float64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT r.name
FROM hotels h
JOIN roads r ON r.region_id = h.region_id
            AND r.geom && ST_Expand(h.geom, 0.002)
            AND ST_DWithin(h.geom::geography, r.geom::geography, $2::float8)
WHERE h.id = $1 AND r.name IS NOT NULL AND r.name <> ''
  AND r.highway IN ('primary', 'secondary', 'tertiary', 'unclassified', 'residential', 'living_street', 'pedestrian')
ORDER BY ST_Distance(h.geom::geography, r.geom::geography), r.id
LIMIT 1`, hotelID, radiusM).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

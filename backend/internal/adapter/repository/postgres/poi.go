package postgres

import (
	"context"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// NearestPoi возвращает для каждого значения pois.kind из kinds ближайшее заведение не дальше radiusM по прямой от отеля.
// Нет заведения — ключа в ответе нет. Выборка идёт по GiST-индексу pois_geom_idx: рамка по градусам отсекает лишнее,
// ST_DWithin по geography даёт точное расстояние в метрах.
func (s *SearchStore) NearestPoi(ctx context.Context, hotelID int64, kinds []string, radiusM float64) (map[string]domain.NearbyPoi, error) {
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT ON (p.kind) p.kind, COALESCE(p.osm_id, 0), COALESCE(p.name, ''), ST_Y(p.geom), ST_X(p.geom),
       ST_Distance(h.geom::geography, p.geom::geography) AS dist
FROM hotels h
JOIN pois p ON p.region_id = h.region_id
           AND p.geom && ST_Expand(h.geom, 0.005)
           AND ST_DWithin(h.geom::geography, p.geom::geography, $3::float8)
WHERE h.id = $1 AND p.kind = ANY($2::text[])
ORDER BY p.kind, dist, p.id`, hotelID, kinds, radiusM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.NearbyPoi{}
	for rows.Next() {
		var n domain.NearbyPoi
		if err := rows.Scan(&n.Kind, &n.OSMID, &n.Name, &n.Lat, &n.Lon, &n.DistM); err != nil {
			return nil, err
		}
		out[n.Kind] = n
	}
	return out, rows.Err()
}

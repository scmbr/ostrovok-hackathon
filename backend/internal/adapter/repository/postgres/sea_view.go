package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// SeaViewStore — пространственные выборки для sea_view и запись фактов.
type SeaViewStore struct {
	pool *pgxpool.Pool
	evidenceWriter
}

func NewSeaViewStore(pool *pgxpool.Pool) *SeaViewStore { return &SeaViewStore{pool: pool, evidenceWriter: evidenceWriter{pool}} }

// SeaViewTargets возвращает отели с фасадами. Этажность фасада — максимум по частям здания
// (building_parts) в пределах FacadeDepthM от фасадной линии, центр которых лежит за стеной
// (башня на подиуме смотрит в ту же сторону поверх подиума), иначе этажность ближайшей части;
// у зданий без частей — этажность здания, а если её нет (высота OSM по умолчанию), высота / 3 м.
func (s *SeaViewStore) SeaViewTargets(ctx context.Context, flt domain.HotelFilter) ([]domain.SeaViewTarget, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, b.id,
       COALESCE(
         (SELECT max(q.floors) FROM (
            SELECT p.floors, ST_Centroid(ST_Transform(p.geom, 25831)) AS c
            FROM building_parts p
            WHERE p.building_id = b.id
              AND ST_DWithin(ST_Transform(p.geom, 25831), ST_Transform(f.geom, 25831), $2::float8)) q
          WHERE (ST_X(q.c) - ST_X(ST_Transform(m.pt, 25831))) * sin(radians(f.azimuth_deg::float8))
              + (ST_Y(q.c) - ST_Y(ST_Transform(m.pt, 25831))) * cos(radians(f.azimuth_deg::float8)) <= 0),
         (SELECT p.floors FROM building_parts p WHERE p.building_id = b.id ORDER BY p.geom <-> m.pt LIMIT 1),
         b.levels, GREATEST(1, ceil(COALESCE(b.height_m, 15) / $1::float8)::int)),
       f.id, ST_X(m.pt), ST_Y(m.pt), f.azimuth_deg
FROM hotels h
JOIN buildings b ON b.id = h.building_id
JOIN facades f ON f.hotel_id = h.id
CROSS JOIN LATERAL (SELECT ST_LineInterpolatePoint(f.geom, 0.5) AS pt) m
WHERE ($3::bigint = 0 OR h.region_id = $3) AND ($4::bigint = 0 OR h.id = $4)
ORDER BY h.id, f.id`, domain.FloorHeightM, domain.FacadeDepthM, flt.RegionID, flt.HotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SeaViewTarget
	for rows.Next() {
		var hotelID, buildingID, facadeID int64
		var levels int
		var lon, lat, az float64
		if err := rows.Scan(&hotelID, &buildingID, &levels, &facadeID, &lon, &lat, &az); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].HotelID != hotelID {
			out = append(out, domain.SeaViewTarget{HotelID: hotelID, BuildingID: buildingID})
		}
		t := &out[len(out)-1]
		t.Facades = append(t.Facades, domain.StoredFacade{ID: facadeID, Mid: orb.Point{lon, lat}, AzimuthDeg: az, Levels: levels})
	}
	return out, rows.Err()
}

// SeaRays: для каждого луча — расстояние до ближайшего пересечения береговой линии, где луч идёт
// с суши в воду (вода справа от направления линии OSM), и здания на пути до берега.
// Препятствия: части зданий кадастра с разной этажностью, здания кадастра без частей и здания OSM,
// не продублированные кадастром (shadowed). Здание самого отеля тоже учитывается.
func (s *SeaViewStore) SeaRays(ctx context.Context, rays []domain.RayQuery, rayMaxM float64) ([]domain.SeaRay, error) {
	lon := make([]float64, len(rays))
	lat := make([]float64, len(rays))
	az := make([]float64, len(rays))
	for i, r := range rays {
		lon[i], lat[i], az[i] = r.Origin[0], r.Origin[1], r.AzimuthDeg
	}
	rows, err := s.pool.Query(ctx, `
WITH q AS (
  SELECT (x.ord - 1)::int AS i,
         ST_Transform(ST_SetSRID(ST_MakePoint(x.lon, x.lat), 4326), 25831) AS o,
         radians(x.az) AS a
  FROM unnest($1::float8[], $2::float8[], $3::float8[]) WITH ORDINALITY AS x(lon, lat, az, ord)
),
r AS (
  SELECT i, o, a,
         ST_MakeLine(o, ST_Translate(o, sin(a) * $4::float8, cos(a) * $4::float8)) AS line
  FROM q
),
cc AS (
  SELECT r.i, r.o, r.a, cp.sg, cp.pt, 2.0 / NULLIF(ST_Length(cp.sg), 0) AS dl
  FROM r
  JOIN sea s ON s.geom && ST_Transform(ST_Expand(r.line, 1), 4326)
  CROSS JOIN LATERAL (
    SELECT t.sg, (ST_Dump(ST_CollectionExtract(ST_Intersection(r.line, t.sg), 1))).geom AS pt
    FROM (SELECT ST_Transform(s.geom, 25831) AS sg) t
  ) cp
),
c AS (
  SELECT z.i, min(ST_Distance(z.o, z.pt)) AS d
  FROM (SELECT cc.*, ST_LineLocatePoint(sg, pt) AS f FROM cc) z
  CROSS JOIN LATERAL (
    SELECT ST_LineInterpolatePoint(z.sg, GREATEST(z.f - z.dl, 0)) AS pa,
           ST_LineInterpolatePoint(z.sg, LEAST(z.f + z.dl, 1)) AS pb
  ) dir
  WHERE (ST_X(dir.pb) - ST_X(dir.pa)) * cos(z.a) - (ST_Y(dir.pb) - ST_Y(dir.pa)) * sin(z.a) < 0
  GROUP BY z.i
)
SELECT r.i, c.d, ob.bid, ob.d, ob.h, ob.hs
FROM r
LEFT JOIN c ON c.i = r.i
LEFT JOIN LATERAL (
  SELECT u.bid, u.h, u.hs, ST_Distance(r.o, ST_Intersection(r.line, ST_MakeValid(u.g))) AS d
  FROM (
    SELECT p.building_id AS bid, p.height_m AS h, $6::text AS hs, ST_Transform(p.geom, 25831) AS g
    FROM building_parts p
    WHERE c.d IS NOT NULL AND ST_Intersects(ST_Transform(p.geom, 25831), r.line)
    UNION ALL
    SELECT b.id, COALESCE(b.height_m, $5::float8), b.height_source, ST_Transform(b.geom, 25831)
    FROM buildings b
    WHERE c.d IS NOT NULL AND NOT b.shadowed
      AND (b.osm_id IS NOT NULL OR NOT EXISTS (SELECT 1 FROM building_parts p WHERE p.building_id = b.id))
      AND ST_Intersects(ST_Transform(b.geom, 25831), r.line)
  ) u
) ob ON ob.d < c.d
ORDER BY r.i, ob.d`, lon, lat, az, rayMaxM, domain.DefaultHeightM, domain.HeightCatastro)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.SeaRay, len(rays))
	for i, r := range rays {
		out[i].AzimuthDeg = r.AzimuthDeg
	}
	for rows.Next() {
		var i int
		var water, dist, height *float64
		var bid *int64
		var source *string
		if err := rows.Scan(&i, &water, &bid, &dist, &height, &source); err != nil {
			return nil, err
		}
		if water != nil {
			out[i].HasWater, out[i].WaterDistM = true, *water
		}
		if bid != nil && dist != nil && height != nil && source != nil {
			out[i].Obstacles = append(out[i].Obstacles, domain.Obstacle{
				BuildingID: *bid, DistM: *dist, HeightM: *height, HeightSource: *source,
			})
		}
	}
	return out, rows.Err()
}


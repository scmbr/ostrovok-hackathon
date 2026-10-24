package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/wkt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// FacadeStore — пространственные выборки для расчёта фасадов и запись результата.
type FacadeStore struct {
	pool *pgxpool.Pool
}

func NewFacadeStore(pool *pgxpool.Pool) *FacadeStore { return &FacadeStore{pool: pool} }

// selfBuilding — здание b дублирует здание отеля own (то же здание из другого источника: кадастр и OSM).
// Такое здание не сосед и не должно давать глухих стен.
const selfBuilding = `(ST_Intersects(b.geom, own.geom)
  AND ST_Area(ST_Intersection(ST_MakeValid(b.geom), ST_MakeValid(own.geom)))
      > 0.5 * LEAST(ST_Area(b.geom), ST_Area(own.geom)))`

func (s *FacadeStore) HotelBuildings(ctx context.Context) ([]domain.HotelBuilding, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, b.id, ST_AsText(b.geom)
FROM hotels h JOIN buildings b ON b.id = h.building_id
ORDER BY h.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HotelBuilding
	for rows.Next() {
		var hb domain.HotelBuilding
		var text string
		if err := rows.Scan(&hb.HotelID, &hb.BuildingID, &text); err != nil {
			return nil, err
		}
		if hb.Geom, err = wkt.UnmarshalMultiPolygon(text); err != nil {
			return nil, fmt.Errorf("контур здания %d: %w", hb.BuildingID, err)
		}
		out = append(out, hb)
	}
	return out, rows.Err()
}

// BlindFractions: для каждого ребра — доля длины в пределах BlindWallDistM от соседних зданий.
func (s *FacadeStore) BlindFractions(ctx context.Context, buildingID int64, edges []domain.Edge) ([]float64, error) {
	lines := make([]string, len(edges))
	for i, e := range edges {
		lines[i] = wkt.MarshalString(orb.LineString{e.A, e.B})
	}
	rows, err := s.pool.Query(ctx, `
WITH own AS (SELECT geom FROM buildings WHERE id = $1),
e AS (
  SELECT (x.ord - 1)::int AS i,
         ST_GeomFromText(x.wkt, 4326) AS g4326,
         ST_Transform(ST_GeomFromText(x.wkt, 4326), 25831) AS g
  FROM unnest($2::text[]) WITH ORDINALITY AS x(wkt, ord)
),
-- Меряем центральную половину ребра, а допуск сужаем до четверти длины: иначе буфер угла соседнего
-- здания, лишь касающегося короткого фасада, накрывал бы его целиком и фасад ошибочно считался глухим.
m AS (
  SELECT i, g4326, ST_LineSubstring(g, 0.25, 0.75) AS core, LEAST($3::float8, 0.25 * ST_Length(g)) AS tol
  FROM e
)
SELECT m.i, COALESCE(
       COALESCE((
         SELECT ST_Length(ST_CollectionExtract(
                  ST_Intersection(m.core, ST_Union(ST_Buffer(ST_MakeValid(ST_Transform(b.geom, 25831)), m.tol))), 2))
         FROM buildings b, own
         WHERE b.id <> $1
           AND b.geom && ST_Expand(m.g4326, 0.0003)
           AND NOT `+selfBuilding+`
       ), 0) / NULLIF(ST_Length(m.core), 0), 0)
FROM m ORDER BY m.i`, buildingID, lines, domain.BlindWallDistM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]float64, 0, len(edges))
	for rows.Next() {
		var i int
		var f float64
		if err := rows.Scan(&i, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FacesStreet: луч от середины фасада наружу на StreetProbeM. Улица — если он пересёк дорогу раньше,
// чем упёрся в здание (соседнее или само здание отеля, если фасад в нише).
func (s *FacadeStore) FacesStreet(ctx context.Context, buildingID int64, probes []domain.Probe) ([]bool, error) {
	lon := make([]float64, len(probes))
	lat := make([]float64, len(probes))
	az := make([]float64, len(probes))
	for i, p := range probes {
		lon[i], lat[i], az[i] = p.Mid[0], p.Mid[1], p.AzimuthDeg
	}
	rows, err := s.pool.Query(ctx, `
WITH own AS (SELECT geom FROM buildings WHERE id = $1),
p AS (
  SELECT (x.ord - 1)::int AS i,
         ST_Transform(ST_SetSRID(ST_MakePoint(x.lon, x.lat), 4326), 25831) AS mid,
         radians(x.az) AS a
  FROM unnest($2::float8[], $3::float8[], $4::float8[]) WITH ORDINALITY AS x(lon, lat, az, ord)
),
pr AS (
  SELECT i, mid,
         ST_MakeLine(ST_Translate(mid, sin(a) * 0.5, cos(a) * 0.5),
                     ST_Translate(mid, sin(a) * $5::float8, cos(a) * $5::float8)) AS probe
  FROM p
)
SELECT pr.i, r.d IS NOT NULL AND (bl.d IS NULL OR r.d < bl.d)
FROM pr
LEFT JOIN LATERAL (
  SELECT min(ST_Distance(pr.mid, ST_Intersection(pr.probe, q.rg))) AS d
  FROM (SELECT ST_Transform(rd.geom, 25831) AS rg FROM roads rd
        WHERE rd.geom && ST_Transform(ST_Expand(pr.probe, 5), 4326)) q
  WHERE ST_Intersects(pr.probe, q.rg)
) r ON true
LEFT JOIN LATERAL (
  SELECT min(ST_Distance(pr.mid, ST_Intersection(pr.probe, q.bg))) AS d
  FROM (SELECT ST_MakeValid(ST_Transform(b.geom, 25831)) AS bg
        FROM buildings b, own
        WHERE b.geom && ST_Transform(ST_Expand(pr.probe, 5), 4326)
          AND (b.id = $1 OR NOT `+selfBuilding+`)) q
  WHERE ST_Intersects(pr.probe, q.bg)
) bl ON true
ORDER BY pr.i`, buildingID, lon, lat, az, domain.StreetProbeM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]bool, 0, len(probes))
	for rows.Next() {
		var i int
		var street bool
		if err := rows.Scan(&i, &street); err != nil {
			return nil, err
		}
		out = append(out, street)
	}
	return out, rows.Err()
}

// ReplaceFacades заменяет фасады отеля и записывает статистику контура.
func (s *FacadeStore) ReplaceFacades(ctx context.Context, hotelID int64, facades []domain.Facade, st domain.ContourStats) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	// Факты о старых фасадах повисли бы на несуществующих id.
	if _, err := tx.Exec(ctx, `
UPDATE facts SET status = $2
WHERE entity_type = $3 AND status = $4 AND entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $1)`,
		hotelID, domain.FactSuperseded, domain.EntityFacade, domain.FactActive); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM facades WHERE hotel_id = $1`, hotelID); err != nil {
		return err
	}
	b := &pgx.Batch{}
	for _, f := range facades {
		b.Queue(`
INSERT INTO facades (hotel_id, azimuth_deg, geom, length_m, faces_street, courtyard)
VALUES ($1, $2, ST_GeomFromText($3, 4326), $4, $5, $6)`,
			hotelID, f.AzimuthDeg, wkt.MarshalString(f.Geom), f.LengthM, f.FacesStreet, f.Courtyard)
	}
	b.Queue(`UPDATE hotels SET contour_segments = $2, blind_segments = $3 WHERE id = $1`, hotelID, st.Segments, st.Blind)
	br := tx.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

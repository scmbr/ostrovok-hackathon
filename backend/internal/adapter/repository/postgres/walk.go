package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/wkb"
	"github.com/paulmach/orb/encoding/wkt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// walkInsertChunk — сколько рёбер вставляется одним запросом.
const walkInsertChunk = 5000

// ReplaceWalkEdges заменяет рёбра пешеходного графа.
func (s *GeoStore) ReplaceWalkEdges(ctx context.Context, edges []domain.WalkEdge) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if _, err := tx.Exec(ctx, `DELETE FROM walk_edges WHERE region_id = $1`, s.regionID); err != nil {
		return err
	}
	for start := 0; start < len(edges); start += walkInsertChunk {
		end := min(start+walkInsertChunk, len(edges))
		n := end - start
		src, dst := make([]int64, 0, n), make([]int64, 0, n)
		geoms, lens := make([]string, 0, n), make([]float64, 0, n)
		for _, e := range edges[start:end] {
			src, dst = append(src, e.Source), append(dst, e.Target)
			geoms, lens = append(geoms, wkt.MarshalString(e.Geom)), append(lens, e.LengthM)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO walk_edges (source, target, geom, length_m, region_id)
SELECT s, t, ST_GeomFromText(g, 4326), l, $5::bigint
FROM unnest($1::bigint[], $2::bigint[], $3::text[], $4::float8[]) AS x(s, t, g, l)`,
			src, dst, geoms, lens, s.regionID); err != nil {
			return fmt.Errorf("рёбра %d–%d из %d: %w", start+1, end, len(edges), err)
		}
	}
	return tx.Commit(ctx)
}

// ReplaceDestinations заменяет цели пеших маршрутов.
func (s *GeoStore) ReplaceDestinations(ctx context.Context, dests []domain.Destination) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if _, err := tx.Exec(ctx, `DELETE FROM destinations WHERE region_id = $1`, s.regionID); err != nil {
		return err
	}
	b := &pgx.Batch{}
	for _, d := range dests {
		b.Queue(`
INSERT INTO destinations (kind, osm_id, name, geom, region_id)
SELECT $1::text, $2::bigint, NULLIF($3::text, ''), t.g, $8::bigint
FROM (SELECT ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326) AS g) t
WHERE $1::text <> $7::text OR EXISTS (SELECT 1 FROM sea s
                          WHERE s.geom && ST_Expand(t.g, 0.003)
                            AND ST_DWithin(s.geom::geography, t.g::geography, $6::float8))`,
			d.Kind, d.OSMID, d.Name, d.Point[0], d.Point[1], domain.BeachMaxFromCoastM, domain.DestBeach, s.regionID)
	}
	br := tx.SendBatch(ctx, b)
	for i := range dests {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("цель %d из %d: %w", i+1, len(dests), err)
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WalkStore — пешеходный граф, цели и привязка точек к графу для расчёта маршрутов.
type WalkStore struct {
	pool *pgxpool.Pool
	evidenceWriter
}

func NewWalkStore(pool *pgxpool.Pool) *WalkStore { return &WalkStore{pool: pool, evidenceWriter: evidenceWriter{pool}} }

// LoadWalkEdges возвращает рёбра графа без геометрии (она нужна только для рёбер маршрута).
func (s *WalkStore) LoadWalkEdges(ctx context.Context, regionID int64) ([]domain.WalkEdge, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, source, target, length_m FROM walk_edges
WHERE $1::bigint = 0 OR region_id = $1`, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WalkEdge
	for rows.Next() {
		var e domain.WalkEdge
		if err := rows.Scan(&e.ID, &e.Source, &e.Target, &e.LengthM); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// HotelPoints возвращает отели с точкой OSM (вход в отель неизвестен).
func (s *WalkStore) HotelPoints(ctx context.Context, flt domain.HotelFilter) ([]domain.HotelPoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT h.id, h.name, ST_X(h.geom), ST_Y(h.geom) FROM hotels h
WHERE ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2) ORDER BY h.id`, flt.RegionID, flt.HotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HotelPoint
	for rows.Next() {
		var h domain.HotelPoint
		var lon, lat float64
		if err := rows.Scan(&h.ID, &h.Name, &lon, &lat); err != nil {
			return nil, err
		}
		h.Point = orb.Point{lon, lat}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Destinations возвращает цели заданного вида.
func (s *WalkStore) Destinations(ctx context.Context, regionID int64, kind string) ([]domain.Destination, error) {
	rows, err := s.pool.Query(ctx, `
SELECT COALESCE(osm_id, 0), COALESCE(name, ''), ST_X(geom), ST_Y(geom) FROM destinations
WHERE kind = $1 AND ($2::bigint = 0 OR region_id = $2) ORDER BY id`, kind, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Destination
	for rows.Next() {
		d := domain.Destination{Kind: kind}
		var lon, lat float64
		if err := rows.Scan(&d.OSMID, &d.Name, &lon, &lat); err != nil {
			return nil, err
		}
		d.Point = orb.Point{lon, lat}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SnapPoints привязывает точки к ближайшему ребру графа не дальше maxM. Результат того же порядка, что и points;
// ok[i] = false, если рядом нет рёбер. Из пяти ближайших по градусам рёбер выбирается ближайшее по метрам.
func (s *WalkStore) SnapPoints(ctx context.Context, points []orb.Point, maxM float64) (snaps []domain.WalkSnap, ok []bool, err error) {
	lon := make([]float64, len(points))
	lat := make([]float64, len(points))
	for i, p := range points {
		lon[i], lat[i] = p[0], p[1]
	}
	snaps = make([]domain.WalkSnap, len(points))
	ok = make([]bool, len(points))
	rows, err := s.pool.Query(ctx, `
SELECT x.ord - 1, e.id, e.source, e.target, e.d, e.frac * e.length_m, (1 - e.frac) * e.length_m
FROM unnest($1::float8[], $2::float8[]) WITH ORDINALITY AS x(lon, lat, ord)
CROSS JOIN LATERAL (
  SELECT c.id, c.source, c.target, c.length_m,
         ST_Distance(c.geom::geography, p.pt::geography) AS d,
         ST_LineLocatePoint(c.geom, p.pt) AS frac
  FROM (SELECT ST_SetSRID(ST_MakePoint(x.lon, x.lat), 4326) AS pt) p
  CROSS JOIN LATERAL (SELECT * FROM walk_edges w ORDER BY w.geom <-> p.pt LIMIT 5) c
  ORDER BY d LIMIT 1
) e
WHERE e.d <= $3::float8`, lon, lat, maxM)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i int64
		var sn domain.WalkSnap
		var d, along, rest float64
		if err := rows.Scan(&i, &sn.EdgeID, &sn.Source, &sn.Target, &d, &along, &rest); err != nil {
			return nil, nil, err
		}
		sn.ToSourceM, sn.ToTargetM = d+along, d+rest
		snaps[i], ok[i] = sn, true
	}
	return snaps, ok, rows.Err()
}

// EdgeGeoms возвращает геометрии рёбер по id.
func (s *WalkStore) EdgeGeoms(ctx context.Context, ids []int64) (map[int64]orb.LineString, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, ST_AsBinary(geom) FROM walk_edges WHERE id = ANY($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]orb.LineString, len(ids))
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		g, err := wkb.Unmarshal(raw)
		if err != nil {
			return nil, err
		}
		ls, isLine := g.(orb.LineString)
		if !isLine {
			return nil, fmt.Errorf("ребро %d: геометрия не линия", id)
		}
		out[id] = ls
	}
	return out, rows.Err()
}


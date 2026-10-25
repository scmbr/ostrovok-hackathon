package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paulmach/orb/encoding/wkt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// NoiseStore — фасадный шум карты BCN: загрузка сегментов и привязка их к фасадам отелей.
type NoiseStore struct {
	pool *pgxpool.Pool
}

func NewNoiseStore(pool *pgxpool.Pool) *NoiseStore { return &NoiseStore{pool: pool} }

// HotelCatastroRefs возвращает кадастровые ссылки зданий отелей.
func (s *NoiseStore) HotelCatastroRefs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT b.catastro_ref
FROM hotels h JOIN buildings b ON b.id = h.building_id
WHERE b.catastro_ref IS NOT NULL
ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// ReplaceNoiseFacades заменяет сегменты фасадного шума; контуры приходят в EPSG:25831.
func (s *NoiseStore) ReplaceNoiseFacades(ctx context.Context, segs []domain.NoiseFacadeSegment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if _, err := tx.Exec(ctx, `DELETE FROM noise_facades`); err != nil {
		return err
	}
	b := &pgx.Batch{}
	for _, g := range segs {
		b.Queue(`
INSERT INTO noise_facades (catastro_ref, geom, ln_db, lden_db, oci_ln_db, map_year)
VALUES ($1, ST_Transform(ST_GeomFromText($2, 25831), 4326), $3, $4, $5, $6)`,
			g.CatastroRef, wkt.MarshalString(g.Line), g.LnDB, g.LdenDB, g.OciLnDB, domain.NoiseMapYear)
	}
	br := tx.SendBatch(ctx, b)
	for i := range segs {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("сегмент %d из %d: %w", i+1, len(segs), err)
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NoiseTargets возвращает отели с фасадами. На каждом фасаде: сегменты карты BCN (если есть) и источники
// шума OSM в радиусе NoiseOSMRadiusM — дороги и ночные заведения с расстоянием и направлением.
func (s *NoiseStore) NoiseTargets(ctx context.Context) ([]domain.NoiseTarget, error) {
	var out []domain.NoiseTarget
	index := map[int64]*domain.NoiseTargetFacade{}

	rows, err := s.pool.Query(ctx, `
SELECT hotel_id, id, azimuth_deg, COALESCE(faces_street, false), courtyard
FROM facades ORDER BY hotel_id, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var hotelID int64
		var f domain.NoiseTargetFacade
		if err := rows.Scan(&hotelID, &f.ID, &f.AzimuthDeg, &f.FacesStreet, &f.Courtyard); err != nil {
			rows.Close()
			return nil, err
		}
		f.OSM.AzimuthDeg = f.AzimuthDeg
		if len(out) == 0 || out[len(out)-1].HotelID != hotelID {
			out = append(out, domain.NoiseTarget{HotelID: hotelID})
		}
		t := &out[len(out)-1]
		t.Facades = append(t.Facades, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Указатели берём после того, как срезы перестали расти.
	for i := range out {
		for j := range out[i].Facades {
			index[out[i].Facades[j].ID] = &out[i].Facades[j]
		}
	}

	// Сегмент карты относится к фасаду, если лежит в полосе NoiseFacadeBufferM вокруг него и длина внутри полосы
	// не меньше min(NoiseMinOverlapM, половина сегмента): соседняя стена в углу сюда не попадает.
	rows, err = s.pool.Query(ctx, `
SELECT f.id, n.ln_db, COALESCE(n.oci_ln_db, 0), o.ov
FROM facades f
JOIN hotels h ON h.id = f.hotel_id
JOIN buildings b ON b.id = h.building_id
JOIN noise_facades n ON n.catastro_ref = b.catastro_ref
CROSS JOIN LATERAL (
  SELECT ST_Length(ST_Intersection(ST_Transform(n.geom, 25831),
                                   ST_Buffer(ST_Transform(f.geom, 25831), $1::float8))) AS ov,
         ST_Length(ST_Transform(n.geom, 25831)) AS len
) o
WHERE ST_DWithin(ST_Transform(f.geom, 25831), ST_Transform(n.geom, 25831), $1::float8)
  AND o.ov >= LEAST($2::float8, o.len / 2)
ORDER BY f.id, n.id`, domain.NoiseFacadeBufferM, domain.NoiseMinOverlapM)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var facadeID int64
		var sample domain.NoiseSample
		if err := rows.Scan(&facadeID, &sample.LnDB, &sample.OciLnDB, &sample.OverlapM); err != nil {
			rows.Close()
			return nil, err
		}
		f := index[facadeID]
		f.Samples = append(f.Samples, sample)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Дороги: ближайший кусок каждой улицы (имя + класс), чтобы длинная улица из нескольких way не считалась дважды.
	// Направление — азимут от середины фасада к ближайшей точке; -1, если точка совпадает с серединой.
	rows, err = s.pool.Query(ctx, `
WITH m AS (
  SELECT f.id AS fid, ST_LineInterpolatePoint(f.geom, 0.5) AS p4326,
         ST_Transform(ST_LineInterpolatePoint(f.geom, 0.5), 25831) AS p,
         radians(f.azimuth_deg::float8) AS az, h.building_id AS b1, h.osm_building_id AS b2
  FROM facades f JOIN hotels h ON h.id = f.hotel_id
)
SELECT DISTINCT ON (m.fid, COALESCE(r.name, r.osm_id::text), r.highway)
       m.fid, r.highway, ST_Distance(c.cp, m.p), COALESCE(degrees(ST_Azimuth(m.p, c.cp)), -1),
       EXISTS (SELECT 1 FROM buildings bl
               WHERE NOT bl.shadowed AND bl.id IS DISTINCT FROM m.b1 AND bl.id IS DISTINCT FROM m.b2
                 AND ST_Intersects(ST_Transform(bl.geom, 25831),
                       ST_MakeLine(ST_Translate(m.p, sin(m.az) * $2::float8, cos(m.az) * $2::float8), c.cp)))
FROM m
JOIN roads r ON r.geom && ST_Expand(m.p4326, 0.003)
CROSS JOIN LATERAL (SELECT ST_ClosestPoint(ST_Transform(r.geom, 25831), m.p) AS cp) c
WHERE ST_DWithin(c.cp, m.p, $1::float8)
ORDER BY m.fid, COALESCE(r.name, r.osm_id::text), r.highway, ST_Distance(c.cp, m.p)`, domain.NoiseOSMRadiusM, domain.RayOriginOffsetM)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var facadeID int64
		var ref domain.NoiseSourceRef
		if err := rows.Scan(&facadeID, &ref.Kind, &ref.DistM, &ref.BearingDeg, &ref.Blocked); err != nil {
			rows.Close()
			return nil, err
		}
		f := index[facadeID]
		f.OSM.Roads = append(f.OSM.Roads, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.pool.Query(ctx, `
WITH m AS (
  SELECT f.id AS fid, ST_LineInterpolatePoint(f.geom, 0.5) AS p4326,
         ST_Transform(ST_LineInterpolatePoint(f.geom, 0.5), 25831) AS p,
         radians(f.azimuth_deg::float8) AS az, h.building_id AS b1, h.osm_building_id AS b2
  FROM facades f JOIN hotels h ON h.id = f.hotel_id
)
SELECT m.fid, q.kind, ST_Distance(q.pp, m.p), COALESCE(degrees(ST_Azimuth(m.p, q.pp)), -1),
       EXISTS (SELECT 1 FROM buildings bl
               WHERE NOT bl.shadowed AND bl.id IS DISTINCT FROM m.b1 AND bl.id IS DISTINCT FROM m.b2
                 AND ST_Intersects(ST_Transform(bl.geom, 25831),
                       ST_MakeLine(ST_Translate(m.p, sin(m.az) * $3::float8, cos(m.az) * $3::float8), q.pp)))
FROM m
JOIN pois q0 ON q0.geom && ST_Expand(m.p4326, 0.003)
CROSS JOIN LATERAL (SELECT q0.kind AS kind, ST_Transform(q0.geom, 25831) AS pp) q
WHERE q.kind = ANY($2::text[]) AND ST_DWithin(q.pp, m.p, $1::float8)
ORDER BY m.fid, q0.id`, domain.NoiseOSMRadiusM, domain.NoiseNightlifeKinds(), domain.RayOriginOffsetM)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var facadeID int64
		var ref domain.NoiseSourceRef
		if err := rows.Scan(&facadeID, &ref.Kind, &ref.DistM, &ref.BearingDeg, &ref.Blocked); err != nil {
			rows.Close()
			return nil, err
		}
		f := index[facadeID]
		f.OSM.Nightlife = append(f.OSM.Nightlife, ref)
	}
	rows.Close()
	return out, rows.Err()
}

// SaveFacts заменяет активные факты и свидетельства noise_night отеля и его фасадов.
func (s *NoiseStore) SaveFacts(ctx context.Context, hotelID int64, facts []domain.Fact) error {
	return saveFacts(ctx, s.pool, hotelID, domain.NoiseNightKey, facts)
}

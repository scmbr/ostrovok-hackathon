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

// NoiseTargets возвращает отели с фасадами, на которые легли сегменты карты: сегмент здания отеля
// относится к фасаду, если лежит в полосе NoiseFacadeBufferM вокруг него и длина внутри полосы не меньше
// min(NoiseMinOverlapM, половина сегмента) — соседняя стена в углу сюда не попадает.
func (s *NoiseStore) NoiseTargets(ctx context.Context) ([]domain.NoiseTarget, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.hotel_id, f.id, f.azimuth_deg, COALESCE(f.faces_street, false), f.courtyard,
       n.ln_db, COALESCE(n.oci_ln_db, 0), o.ov
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
ORDER BY f.hotel_id, f.id, n.id`, domain.NoiseFacadeBufferM, domain.NoiseMinOverlapM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NoiseTarget
	for rows.Next() {
		var hotelID, facadeID int64
		var az float64
		var street, courtyard bool
		var sample domain.NoiseSample
		if err := rows.Scan(&hotelID, &facadeID, &az, &street, &courtyard, &sample.LnDB, &sample.OciLnDB, &sample.OverlapM); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].HotelID != hotelID {
			out = append(out, domain.NoiseTarget{HotelID: hotelID})
		}
		t := &out[len(out)-1]
		if len(t.Facades) == 0 || t.Facades[len(t.Facades)-1].ID != facadeID {
			t.Facades = append(t.Facades, domain.NoiseTargetFacade{ID: facadeID, AzimuthDeg: az, FacesStreet: street, Courtyard: courtyard})
		}
		f := &t.Facades[len(t.Facades)-1]
		f.Samples = append(f.Samples, sample)
	}
	return out, rows.Err()
}

// SaveFacts заменяет активные факты и свидетельства noise_night отеля и его фасадов.
func (s *NoiseStore) SaveFacts(ctx context.Context, hotelID int64, facts []domain.Fact) error {
	return saveFacts(ctx, s.pool, hotelID, domain.NoiseNightKey, facts)
}

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/wkt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// GeoStore сохраняет собранные геоданные, отели и моки карточек.
type GeoStore struct {
	pool *pgxpool.Pool
}

func NewGeoStore(pool *pgxpool.Pool) *GeoStore { return &GeoStore{pool: pool} }

// UpsertBuildings вставляет или обновляет здания по osm_id.
func (s *GeoStore) UpsertBuildings(ctx context.Context, buildings []domain.Building) error {
	const q = `
INSERT INTO buildings (osm_id, geom, levels, height_m, height_source)
VALUES ($1, ST_Multi(ST_GeomFromText($2, 4326)), $3, $4, $5)
ON CONFLICT (osm_id) DO UPDATE
SET geom = EXCLUDED.geom, levels = EXCLUDED.levels,
    height_m = EXCLUDED.height_m, height_source = EXCLUDED.height_source`
	return s.batch(ctx, len(buildings), func(b *pgx.Batch, i int) {
		bl := buildings[i]
		b.Queue(q, bl.OSMID, wkt.MarshalString(bl.Geom), bl.Levels, bl.HeightM, bl.HeightSource)
	})
}

// UpsertHotels сохраняет отели. Здания привязывает LinkHotels. Мок карточки не затирается.
func (s *GeoStore) UpsertHotels(ctx context.Context, hotels []domain.Hotel) error {
	const q = `
INSERT INTO hotels (osm_id, name, geom, district)
VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5)
ON CONFLICT (osm_id) DO UPDATE
SET name = EXCLUDED.name, geom = EXCLUDED.geom, district = EXCLUDED.district`
	return s.batch(ctx, len(hotels), func(b *pgx.Batch, i int) {
		h := hotels[i]
		b.Queue(q, h.StoreID(), h.Name, h.Point[0], h.Point[1], h.District)
	})
}

// UpsertCatastroBuildings вставляет или обновляет здания кадастра по catastro_ref.
// Контуры приходят в EPSG:25831 и переводятся в 4326 в PostGIS.
func (s *GeoStore) UpsertCatastroBuildings(ctx context.Context, buildings []domain.Building) error {
	const q = `
INSERT INTO buildings (catastro_ref, geom, levels, height_m, height_source)
VALUES ($1, ST_Multi(ST_Transform(ST_GeomFromText($2, 25831), 4326)), $3, $4, $5)
ON CONFLICT (catastro_ref) WHERE catastro_ref IS NOT NULL DO UPDATE
SET geom = EXCLUDED.geom, levels = EXCLUDED.levels,
    height_m = EXCLUDED.height_m, height_source = EXCLUDED.height_source`
	return s.batch(ctx, len(buildings), func(b *pgx.Batch, i int) {
		bl := buildings[i]
		b.Queue(q, bl.CatastroRef, wkt.MarshalString(bl.Geom), bl.Levels, bl.HeightM, bl.HeightSource)
	})
}

// LinkHotels привязывает отели к зданиям. osm_building_id — здание OSM: way отеля или первое
// здание, содержащее точку. building_id — здание кадастра, содержащее точку, иначе ближайшее
// в пределах catastroSnapM, иначе osm_building_id. Можно вызывать после любой из загрузок.
func (s *GeoStore) LinkHotels(ctx context.Context) error {
	const catastroSnapM = 15
	_, err := s.pool.Exec(ctx, `
UPDATE hotels h SET
  osm_building_id = o.id,
  building_id = COALESCE(c.id, o.id)
FROM hotels h2
LEFT JOIN LATERAL (
  SELECT b.id FROM buildings b
  WHERE b.osm_id IS NOT NULL
    AND (b.osm_id = -h2.osm_id OR ST_Contains(b.geom, h2.geom))
  ORDER BY (b.osm_id = -h2.osm_id) DESC, b.id LIMIT 1
) o ON true
LEFT JOIN LATERAL (
  SELECT b.id FROM buildings b
  WHERE b.catastro_ref IS NOT NULL
    AND (ST_Contains(b.geom, h2.geom) OR ST_DWithin(b.geom::geography, h2.geom::geography, $1))
  ORDER BY ST_Contains(b.geom, h2.geom) DESC, b.geom <-> h2.geom LIMIT 1
) c ON true
WHERE h.id = h2.id`, catastroSnapM)
	return err
}

// ReplaceSea заменяет береговую линию целиком: у неё нет собственного ключа.
func (s *GeoStore) ReplaceSea(ctx context.Context, lines []orb.LineString) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if _, err := tx.Exec(ctx, `DELETE FROM sea`); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := tx.Exec(ctx, `INSERT INTO sea (geom) VALUES (ST_GeomFromText($1, 4326))`, wkt.MarshalString(l)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *GeoStore) ListHotels(ctx context.Context) ([]domain.HotelKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, osm_id FROM hotels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HotelKey
	for rows.Next() {
		var k domain.HotelKey
		if err := rows.Scan(&k.ID, &k.OSMID); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SaveMockCard пишет карточку и, если у отеля ещё нет категорий номеров, создаёт их из карточки.
// facade_ids остаются пустыми: их заполняет отельер.
func (s *GeoStore) SaveMockCard(ctx context.Context, hotelID int64, card domain.Card) error {
	raw, err := json.Marshal(card)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if _, err := tx.Exec(ctx, `UPDATE hotels SET card = $2 WHERE id = $1`, hotelID, raw); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO room_categories (hotel_id, name)
SELECT $1, name FROM unnest($2::text[]) AS name
WHERE NOT EXISTS (SELECT 1 FROM room_categories WHERE hotel_id = $1)`, hotelID, card.RoomCategories); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *GeoStore) ListDemoCandidates(ctx context.Context) ([]domain.DemoCandidate, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, COALESCE(h.district, ''), h.building_id IS NOT NULL,
       COALESCE(b.height_source <> $1, false),
       EXISTS (SELECT 1 FROM sea WHERE ST_DWithin(sea.geom::geography, h.geom::geography, $2))
FROM hotels h LEFT JOIN buildings b ON b.id = h.building_id`, domain.HeightDefault, domain.CoastalRadiusM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DemoCandidate
	for rows.Next() {
		var c domain.DemoCandidate
		if err := rows.Scan(&c.ID, &c.District, &c.HasBuilding, &c.KnownHeight, &c.Coastal); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *GeoStore) SetDemo(ctx context.Context, ids []int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE hotels SET demo = (id = ANY($1))`, ids)
	return err
}

// Stats собирает сводку для проверки критерия готовности.
func (s *GeoStore) Stats(ctx context.Context) (domain.Stats, error) {
	st := domain.Stats{HotelsByDistrict: map[string]int{}, BuildingsBySource: map[string]int{}}
	if err := s.countBy(ctx, `SELECT COALESCE(district, ''), count(*) FROM hotels GROUP BY 1`, st.HotelsByDistrict); err != nil {
		return st, err
	}
	if err := s.countBy(ctx, `SELECT height_source, count(*) FROM buildings GROUP BY 1`, st.BuildingsBySource); err != nil {
		return st, err
	}
	err := s.pool.QueryRow(ctx, `
SELECT count(h.building_id),
       count(*) FILTER (WHERE b.height_source <> $1),
       count(*) FILTER (WHERE b.height_source = $2),
       count(h.osm_building_id),
       count(*) FILTER (WHERE b.height_source = $2 AND o.height_source IN ($3, $4)
                        AND round(b.height_m / $5) <> round(o.height_m / $5)),
       count(*) FILTER (WHERE coastal),
       count(*) FILTER (WHERE coastal AND h.demo)
FROM (SELECT h.*, EXISTS (SELECT 1 FROM sea WHERE ST_DWithin(sea.geom::geography, h.geom::geography, $6)) AS coastal
      FROM hotels h) h
LEFT JOIN buildings b ON b.id = h.building_id
LEFT JOIN buildings o ON o.id = h.osm_building_id`,
		domain.HeightDefault, domain.HeightCatastro, domain.HeightOSMHeight, domain.HeightOSMLevels,
		domain.MetersPerLevel, domain.CoastalRadiusM).
		Scan(&st.HotelsWithBuilding, &st.HotelsWithKnownHeight, &st.HotelsWithCatastro,
			&st.HotelsWithOSMBuilding, &st.HotelsHeightDiffers, &st.CoastalHotels, &st.CoastalDemoHotels)
	if err != nil {
		return st, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sea`).Scan(&st.SeaSegments); err != nil {
		return st, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM hotels WHERE demo`).Scan(&st.DemoHotels); err != nil {
		return st, err
	}
	return st, nil
}

func (s *GeoStore) countBy(ctx context.Context, q string, into map[string]int) error {
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		into[k] = n
	}
	return rows.Err()
}

// batch выполняет n запросов одним пакетом в транзакции.
func (s *GeoStore) batch(ctx context.Context, n int, queue func(b *pgx.Batch, i int)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	b := &pgx.Batch{}
	for i := 0; i < n; i++ {
		queue(b, i)
	}
	br := tx.SendBatch(ctx, b)
	for i := 0; i < n; i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("запись %d из %d: %w", i+1, n, err)
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

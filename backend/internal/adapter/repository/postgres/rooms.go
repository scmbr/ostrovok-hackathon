package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/collect"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// RoomStore — категории номеров, их размещение и свидетельства о категориях.
type RoomStore struct {
	pool *pgxpool.Pool
	evidenceWriter
}

func NewRoomStore(pool *pgxpool.Pool, rules map[string]domain.TruthParams) *RoomStore {
	return &RoomStore{pool: pool, evidenceWriter: evidenceWriter{pool: pool, rules: rules}}
}

func categoryScope(categoryID int64) evidenceScope {
	return evidenceScope{strconv.FormatInt(categoryID, 10),
		`characteristic_key = $2 AND entity_type = 'room_category' AND entity_id = $1::text`}
}

// HotelsWithCategories — отели, у которых есть размещённые категории.
func (s *RoomStore) HotelsWithCategories(ctx context.Context, flt domain.HotelFilter) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id FROM hotels h
WHERE ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2)
  AND EXISTS (SELECT 1 FROM room_categories c JOIN room_placements p ON p.room_category_id = c.id WHERE c.hotel_id = h.id)
ORDER BY h.id`, flt.RegionID, flt.HotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Categories читает категории отеля вместе с размещениями.
func (s *RoomStore) Categories(ctx context.Context, hotelID int64) ([]domain.RoomCategory, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, hotel_id, name, rooms_count FROM room_categories WHERE hotel_id = $1 ORDER BY id`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RoomCategory
	idx := map[int64]int{}
	for rows.Next() {
		var c domain.RoomCategory
		if err := rows.Scan(&c.ID, &c.HotelID, &c.Name, &c.Rooms); err != nil {
			return nil, err
		}
		idx[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prows, err := s.pool.Query(ctx, `
SELECT p.id, p.room_category_id, p.facade_id, p.floor_from, p.floor_to, p.rooms_count, p.source, p.confidence, COALESCE(p.evidence_id, 0)
FROM room_placements p JOIN room_categories c ON c.id = p.room_category_id
WHERE c.hotel_id = $1 ORDER BY p.id`, hotelID)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var p domain.StoredPlacement
		var cat int64
		if err := prows.Scan(&p.ID, &cat, &p.FacadeID, &p.FloorFrom, &p.FloorTo, &p.Rooms, &p.Source, &p.Confidence, &p.EvidenceID); err != nil {
			return nil, err
		}
		out[idx[cat]].Placements = append(out[idx[cat]].Placements, p)
	}
	return out, prows.Err()
}

// Facades читает фасады отеля.
func (s *RoomStore) Facades(ctx context.Context, hotelID int64) ([]rooms.FacadeInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, azimuth_deg, COALESCE(faces_street, false), COALESCE(courtyard, false)
FROM facades WHERE hotel_id = $1 ORDER BY id`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rooms.FacadeInfo
	for rows.Next() {
		var f rooms.FacadeInfo
		if err := rows.Scan(&f.ID, &f.AzimuthDeg, &f.FacesStreet, &f.Courtyard); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FacadeFacts читает активные факты фасадов отеля по характеристикам keys.
func (s *RoomStore) FacadeFacts(ctx context.Context, hotelID int64, keys []string) ([]domain.Fact, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.entity_type, f.entity_id, f.characteristic_key, f.value, f.source, f.method, f.confidence, f.observed_at, f.valid_until
FROM facts f
WHERE f.status = $3 AND f.entity_type = 'facade' AND f.characteristic_key = ANY($2)
  AND f.entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $1)
ORDER BY f.id`, hotelID, keys, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Fact
	for rows.Next() {
		var f domain.Fact
		var value []byte
		if err := rows.Scan(&f.ID, &f.EntityType, &f.EntityID, &f.Key, &value, &f.Source, &f.Method, &f.Confidence,
			&f.ObservedAt, &f.ValidUntil); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(value, &f.Value); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CategoryFacts читает активные факты категорий отеля с открытым конфликтом и свидетельствами.
func (s *RoomStore) CategoryFacts(ctx context.Context, hotelID int64) ([]rooms.CategoryFact, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.entity_id::bigint, f.characteristic_key, f.value, f.source, f.method, f.confidence, f.observed_at, f.valid_until,
       f.weak_disagreement, f.stale, f.evidence_ids, c.description
FROM facts f
LEFT JOIN conflicts c ON f.id = ANY(c.fact_ids) AND c.status = $2
WHERE f.status = $3 AND f.entity_type = 'room_category'
  AND f.entity_id IN (SELECT id::text FROM room_categories WHERE hotel_id = $1)
ORDER BY f.id`, hotelID, domain.ConflictOpen, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rooms.CategoryFact
	for rows.Next() {
		var cf rooms.CategoryFact
		var value []byte
		var conflict *string
		f := &cf.Fact
		if err := rows.Scan(&f.ID, &cf.CategoryID, &f.Key, &value, &f.Source, &f.Method, &f.Confidence, &f.ObservedAt, &f.ValidUntil,
			&f.WeakDisagreement, &f.Stale, &f.EvidenceIDs, &conflict); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(value, &f.Value); err != nil {
			return nil, err
		}
		if conflict != nil {
			f.Conflict = &domain.Conflict{Description: *conflict}
		}
		f.EntityType, f.EntityID = domain.EntityRoomCategory, strconv.FormatInt(cf.CategoryID, 10)
		out = append(out, cf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if out[i].Evidence, err = s.evidenceViews(ctx, out[i].Fact.EvidenceIDs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *RoomStore) evidenceViews(ctx context.Context, ids []int64) ([]rooms.EvidenceView, error) {
	return loadEvidenceViews(ctx, s.pool, ids)
}

// loadEvidenceViews читает свидетельства по id вместе с вкладом в факт.
func loadEvidenceViews(ctx context.Context, pool *pgxpool.Pool, ids []int64) ([]rooms.EvidenceView, error) {
	rows, err := pool.Query(ctx, `
SELECT e.id, e.source, e.author_type, e.author_id, COALESCE(t.trust, $2), e.claimed_value, e.reliability, e.contribution, e.observed_at
FROM evidence e
LEFT JOIN author_trust t ON t.author_type = e.author_type AND t.author_id = e.author_id
WHERE e.id = ANY($1) ORDER BY e.id`, ids, domain.DefaultTrust)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rooms.EvidenceView
	for rows.Next() {
		var e rooms.EvidenceView
		var claimed []byte
		if err := rows.Scan(&e.ID, &e.Source, &e.AuthorType, &e.AuthorID, &e.AuthorTrust, &claimed, &e.Reliability, &e.Contribution, &e.ObservedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(claimed, &e.Claimed); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveCategoryEvidence записывает свидетельства характеристики key о категории и пересобирает её факт, см. saveEvidence.
func (s *RoomStore) SaveCategoryEvidence(ctx context.Context, categoryID int64, key string, authors []domain.Author, evs []domain.Evidence) error {
	return saveEvidence(ctx, s.pool, s.rules, categoryScope(categoryID), key, authors, evs)
}

// RoomMockTargets возвращает демо-отели с фасадами, которым генерируются категории и размещение.
// Этажность и этаж начала вида берутся из фактов sea_view по фасадам.
func (s *RoomStore) RoomMockTargets(ctx context.Context, flt domain.HotelFilter) ([]collect.RoomMockTarget, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, h.name, COALESCE(h.card->'room_categories', '[]'::jsonb),
       EXISTS (SELECT 1 FROM sea WHERE ST_DWithin(sea.geom::geography, h.geom::geography, $3)),
       f.id, COALESCE(f.faces_street, false), (x.value->>'levels')::int, (x.value->>'min_floor')::int
FROM hotels h
JOIN facades f ON f.hotel_id = h.id
JOIN facts x ON x.entity_type = 'facade' AND x.entity_id = f.id::text AND x.characteristic_key = $4 AND x.status = $5
WHERE h.demo AND ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2)
ORDER BY h.id, f.id`, flt.RegionID, flt.HotelID, domain.CoastalRadiusM, domain.SeaViewKey, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collect.RoomMockTarget
	for rows.Next() {
		var id, facadeID int64
		var name string
		var cats []byte
		var coastal, street bool
		var levels int
		var minFloor *int
		if err := rows.Scan(&id, &name, &cats, &coastal, &facadeID, &street, &levels, &minFloor); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].HotelID != id {
			t := collect.RoomMockTarget{HotelID: id, Name: name, Coastal: coastal}
			if err := json.Unmarshal(cats, &t.Categories); err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		mf := 0
		if minFloor != nil {
			mf = *minFloor
		}
		t := &out[len(out)-1]
		t.Facades = append(t.Facades, collect.MockFacade{ID: facadeID, FacesStreet: street, Levels: levels, ViewFromFloor: mf})
	}
	return out, rows.Err()
}

// SaveHotelRooms заменяет категории отеля из набора: категории и их размещение, свидетельство отельера
// room_placement (одно на категорию, строки размещения ссылаются на него) и утверждение названия («Sea View»).
// Факты категорий пересобираются движком правды; расчётные свидетельства категорий сохраняются.
// Всё в одной транзакции. Название категории добавляется в мок-карточку, если его там нет.
func (s *RoomStore) SaveHotelRooms(ctx context.Context, hotelID int64, in []collect.RoomsInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден

	author := domain.HotelierAuthor(hotelID)
	authors := []domain.Author{author}
	now := time.Now()
	until := now.AddDate(0, 0, domain.RoomPlacementTTLDays)
	names := make([]string, 0, len(in))
	for _, r := range in {
		names = append(names, r.Name)
		var catID int64
		if err := tx.QueryRow(ctx, `
INSERT INTO room_categories (hotel_id, name, rooms_count) VALUES ($1, $2, $3)
ON CONFLICT (hotel_id, name) DO UPDATE SET rooms_count = EXCLUDED.rooms_count
RETURNING id`, hotelID, r.Name, r.Rooms()).Scan(&catID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM room_placements WHERE room_category_id = $1`, catID); err != nil {
			return err
		}
		entityID := strconv.FormatInt(catID, 10)

		placed := make([]map[string]any, len(r.Placements))
		for i, p := range r.Placements {
			placed[i] = map[string]any{"facade_id": p.FacadeID, "floor_from": p.FloorFrom, "floor_to": p.FloorTo, "rooms_count": p.Rooms}
		}
		placement := []domain.Evidence{{
			EntityType: domain.EntityRoomCategory, EntityID: entityID, Key: domain.RoomPlacementKey,
			Claimed: map[string]any{"placements": placed, "rooms_count": r.Rooms()},
			Source:  domain.SourceHotelier, Method: "hotelier_answer", AuthorType: author.Type, AuthorID: author.ID,
			Reliability: domain.HotelierBaseReliability, ObservedAt: now, ValidUntil: &until,
			Details: map[string]any{"persona": r.Persona, "note": "размещение номеров сообщает отельер (мок)"},
		}}
		if err := saveEvidenceTx(ctx, tx, s.rules, categoryScope(catID), domain.RoomPlacementKey, authors, placement); err != nil {
			return err
		}
		for _, p := range r.Placements {
			if _, err := tx.Exec(ctx, `
INSERT INTO room_placements (room_category_id, facade_id, floor_from, floor_to, rooms_count, source, confidence, evidence_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				catID, p.FacadeID, p.FloorFrom, p.FloorTo, p.Rooms, domain.SourceHotelier, domain.HotelierBaseReliability, placement[0].ID); err != nil {
				return err
			}
		}

		// Название категории — утверждение отеля о виде: свидетельство «full» с асимметрией, проверяется расчётом.
		var claims []domain.Evidence
		if domain.TitleClaimsSeaView(r.Name) {
			claims = append(claims, domain.Evidence{
				EntityType: domain.EntityRoomCategory, EntityID: entityID, Key: domain.SeaViewKey,
				Claimed: map[string]any{"view": "full"},
				Source:  domain.SourceHotelier, Method: "category_title", AuthorType: author.Type, AuthorID: author.ID,
				Reliability: domain.HotelierBaseReliability, ObservedAt: now, ValidUntil: &until,
				Details: map[string]any{"title": r.Name, "note": "вид на море заявлен названием категории"},
			})
		}
		if err := saveEvidenceTx(ctx, tx, s.rules, categoryScope(catID), domain.SeaViewKey, authors, claims); err != nil {
			return err
		}
	}
	if err := addCardCategories(ctx, tx, hotelID, names); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// addCardCategories дописывает названия категорий в мок-карточку отеля (порядок карточки сохраняется);
// карточки не из мока не трогает.
func addCardCategories(ctx context.Context, tx pgx.Tx, hotelID int64, names []string) error {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(card->'room_categories', '[]'::jsonb) FROM hotels WHERE id = $1 AND card ? 'mock'`,
		hotelID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var cur []string
	if err := json.Unmarshal(raw, &cur); err != nil {
		return err
	}
	for _, n := range names {
		if !slices.Contains(cur, n) {
			cur = append(cur, n)
		}
	}
	out, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE hotels SET card = jsonb_set(card, '{room_categories}', $2::jsonb) WHERE id = $1`, hotelID, string(out))
	return err
}

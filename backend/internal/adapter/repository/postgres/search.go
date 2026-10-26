package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// SearchSource — источник спроса, который пополняет поиск (characteristic_demand.source).
const SearchSource = "search"

// SearchStore — хранилище поиска: отели с мок-карточкой, факты отеля со свидетельствами, запись спроса.
type SearchStore struct{ pool *pgxpool.Pool }

func NewSearchStore(pool *pgxpool.Pool) *SearchStore { return &SearchStore{pool} }

const candidateSelect = `SELECT h.id, h.name, COALESCE(h.district, ''), h.card, ST_Y(h.geom), ST_X(h.geom) FROM hotels h`

func scanCandidate(row pgx.Row) (search.Candidate, error) {
	var c search.Candidate
	var card []byte
	if err := row.Scan(&c.ID, &c.Name, &c.District, &card, &c.Lat, &c.Lon); err != nil {
		return c, err
	}
	if err := json.Unmarshal(card, &c.Card); err != nil {
		return c, fmt.Errorf("карточка отеля %d: %w", c.ID, err)
	}
	return c, nil
}

// Candidates возвращает демо-отели региона с размещёнными категориями номеров.
func (s *SearchStore) Candidates(ctx context.Context, regionID int64) ([]search.Candidate, error) {
	rows, err := s.pool.Query(ctx, candidateSelect+`
WHERE h.region_id = $1 AND h.demo
  AND EXISTS (SELECT 1 FROM room_categories c JOIN room_placements p ON p.room_category_id = c.id WHERE c.hotel_id = h.id)
ORDER BY h.id`, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []search.Candidate
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Candidate возвращает один отель по id.
func (s *SearchStore) Candidate(ctx context.Context, id int64) (search.Candidate, error) {
	c, err := scanCandidate(s.pool.QueryRow(ctx, candidateSelect+` WHERE h.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, search.ErrNotFound
	}
	return c, err
}

// HotelFacts читает активные факты отеля и его района, открытые конфликты и свидетельства.
func (s *SearchStore) HotelFacts(ctx context.Context, hotelID int64) ([]search.FactView, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.entity_type, f.entity_id, f.characteristic_key, f.value, f.source, f.method, f.confidence,
       f.observed_at, f.valid_until, f.weak_disagreement, f.stale, f.evidence_ids, c.description
FROM facts f
LEFT JOIN conflicts c ON f.id = ANY(c.fact_ids) AND c.status = $2
WHERE f.status = $3 AND (
      (f.entity_type = 'hotel' AND f.entity_id = ($1::bigint)::text)
   OR (f.entity_type = 'district' AND f.entity_id = (SELECT district FROM hotels WHERE id = $1::bigint)))
ORDER BY f.id`, hotelID, domain.ConflictOpen, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []search.FactView
	var ids []int64
	for rows.Next() {
		var f domain.Fact
		var value []byte
		var conflict *string
		if err := rows.Scan(&f.ID, &f.EntityType, &f.EntityID, &f.Key, &value, &f.Source, &f.Method, &f.Confidence,
			&f.ObservedAt, &f.ValidUntil, &f.WeakDisagreement, &f.Stale, &f.EvidenceIDs, &conflict); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(value, &f.Value); err != nil {
			return nil, err
		}
		if conflict != nil {
			f.Conflict = &domain.Conflict{Description: *conflict}
		}
		out = append(out, search.FactView{Fact: f})
		ids = append(ids, f.EvidenceIDs...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return out, nil
	}
	evs, err := loadEvidenceViews(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]rooms.EvidenceView, len(evs))
	for _, e := range evs {
		byID[e.ID] = e
	}
	for i := range out {
		for _, id := range out[i].Fact.EvidenceIDs {
			if e, ok := byID[id]; ok {
				out[i].Evidence = append(out[i].Evidence, e)
			}
		}
	}
	return out, nil
}

// Record увеличивает спрос по ключам желаний (source = 'search'). Сессия увеличивает спрос по ключу один раз;
// пустая сессия — каждый запрос сам по себе.
func (s *SearchStore) Record(ctx context.Context, regionID int64, sessionID string, keys []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, key := range keys {
		if sessionID != "" {
			tag, err := tx.Exec(ctx, `
INSERT INTO demand_sessions (session_id, characteristic_key, region_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				sessionID, key, regionID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO characteristic_demand (characteristic_key, region_id, count, source) VALUES ($1, $2, 1, $3)
ON CONFLICT (characteristic_key, region_id, source) DO UPDATE SET count = characteristic_demand.count + 1`,
			key, regionID, SearchSource); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

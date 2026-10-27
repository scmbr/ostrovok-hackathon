package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/reputation"
)

// ReputationStore хранит репутацию авторов и читает их утверждения для сравнения.
type ReputationStore struct {
	pool  *pgxpool.Pool
	rules map[string]domain.TruthParams
}

func NewReputationStore(pool *pgxpool.Pool, rules map[string]domain.TruthParams) *ReputationStore {
	return &ReputationStore{pool: pool, rules: rules}
}

const evidenceCols = `id, entity_type, entity_id, characteristic_key, claimed_value, source, method,
       author_type, author_id, reliability, observed_at, valid_until, details`

func scanEvidence(rows pgx.Rows) ([]domain.Evidence, error) {
	defer rows.Close()
	var out []domain.Evidence
	for rows.Next() {
		var e domain.Evidence
		var claimed, details []byte
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.Key, &claimed, &e.Source, &e.Method,
			&e.AuthorType, &e.AuthorID, &e.Reliability, &e.ObservedAt, &e.ValidUntil, &details); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(claimed, &e.Claimed); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *ReputationStore) AuthorClaims(ctx context.Context, a domain.Author) ([]reputation.Claim, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+evidenceCols+` FROM evidence
WHERE status = $1 AND author_type = $2 AND author_id = $3 ORDER BY id`, domain.FactActive, a.Type, a.ID)
	if err != nil {
		return nil, err
	}
	mine, err := scanEvidence(rows)
	if err != nil {
		return nil, err
	}
	out := make([]reputation.Claim, 0, len(mine))
	for _, e := range mine {
		rows, err := s.pool.Query(ctx, `SELECT `+evidenceCols+` FROM evidence
WHERE status = $1 AND entity_type = $2 AND entity_id = $3 AND characteristic_key = $4
  AND NOT (author_type = $5 AND author_id = $6) ORDER BY id`, domain.FactActive, e.EntityType, e.EntityID, e.Key, a.Type, a.ID)
		if err != nil {
			return nil, err
		}
		others, err := scanEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, reputation.Claim{Evidence: e, Others: others})
	}
	return out, nil
}

// Authors — авторы типа authorType, у которых есть активные свидетельства или запись репутации.
func (s *ReputationStore) Authors(ctx context.Context, authorType string) ([]domain.Author, error) {
	rows, err := s.pool.Query(ctx, `
SELECT author_id FROM evidence WHERE status = $2 AND author_type = $1
UNION SELECT author_id FROM author_trust WHERE author_type = $1 ORDER BY 1`, authorType, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Author
	for rows.Next() {
		a := domain.Author{Type: authorType}
		if err := rows.Scan(&a.ID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *ReputationStore) Trust(ctx context.Context, a domain.Author) (int, int, float64, bool, error) {
	var agree, total int
	var trust float64
	err := s.pool.QueryRow(ctx, `SELECT agree, total, trust FROM author_trust WHERE author_type = $1 AND author_id = $2`,
		a.Type, a.ID).Scan(&agree, &total, &trust)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, domain.DefaultTrust, false, nil
	}
	return agree, total, trust, err == nil, err
}

func (s *ReputationStore) SaveTrust(ctx context.Context, a domain.Author, agree, total int, trust float64) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO author_trust (author_type, author_id, agree, total, trust, updated_at) VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (author_type, author_id) DO UPDATE SET agree = EXCLUDED.agree, total = EXCLUDED.total,
                                                   trust = EXCLUDED.trust, updated_at = now()`,
		a.Type, a.ID, agree, total, trust)
	return err
}

// RefreshFacts пересобирает факты по всем характеристикам и сущностям, где у автора есть активное свидетельство.
// Репутация входит в надёжность свидетельства, поэтому при её изменении факты меняются. Свидетельства отельера
// лежат в области отеля (сам отель и его фасады) или категории номеров.
func (s *ReputationStore) RefreshFacts(ctx context.Context, a domain.Author) error {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT entity_type, entity_id, characteristic_key FROM evidence
WHERE status = $1 AND author_type = $2 AND author_id = $3`, domain.FactActive, a.Type, a.ID)
	if err != nil {
		return err
	}
	type target struct{ entityType, entityID, key string }
	var ts []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.entityType, &t.entityID, &t.key); err != nil {
			rows.Close()
			return err
		}
		ts = append(ts, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	done := map[target]bool{}
	for _, t := range ts {
		var scope evidenceScope
		switch {
		case t.entityType == domain.EntityRoomCategory:
			id, err := strconv.ParseInt(t.entityID, 10, 64)
			if err != nil {
				return err
			}
			scope = categoryScope(id)
		case a.Type == domain.AuthorHotelier:
			id, err := strconv.ParseInt(a.ID, 10, 64)
			if err != nil {
				return err
			}
			scope = hotelScope(id)
			t.entityType, t.entityID = domain.EntityHotel, a.ID
		default:
			continue // область автора-гостя придёт вместе с анкетами (5.3)
		}
		if done[t] {
			continue
		}
		done[t] = true
		if err := saveEvidence(ctx, s.pool, s.rules, scope, t.key, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

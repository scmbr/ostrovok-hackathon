package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// TruthStore — пересборка фактов из активных свидетельств всех областей (collect truth).
type TruthStore struct{ pool *pgxpool.Pool }

func NewTruthStore(pool *pgxpool.Pool) *TruthStore { return &TruthStore{pool} }

// ActiveEvidence читает все активные свидетельства в порядке записи.
func (s *TruthStore) ActiveEvidence(ctx context.Context) ([]domain.Evidence, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, entity_type, entity_id, characteristic_key, claimed_value, source, method,
       author_type, author_id, reliability, observed_at, valid_until, details
FROM evidence WHERE status = $1 ORDER BY id`, domain.FactActive)
	if err != nil {
		return nil, err
	}
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

// AuthorTrust читает репутацию авторов-людей.
func (s *TruthStore) AuthorTrust(ctx context.Context) (map[domain.Author]float64, error) {
	return loadTrust(ctx, s.pool)
}

// ReplaceFacts в одной транзакции снимает активные факты и открытые конфликты затронутых характеристик,
// пишет новые факты (с конфликтами) и вклады свидетельств.
func (s *TruthStore) ReplaceFacts(ctx context.Context, facts []domain.Fact) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден

	seen := map[string]bool{}
	var keys []string
	for _, f := range facts {
		if !seen[f.Key] {
			seen[f.Key] = true
			keys = append(keys, f.Key)
		}
	}
	for _, q := range []struct{ table, to, from string }{
		{"facts", domain.FactSuperseded, domain.FactActive},
		{"conflicts", domain.ConflictSuperseded, domain.ConflictOpen},
	} {
		if _, err := tx.Exec(ctx, `UPDATE `+q.table+` SET status = $1 WHERE status = $2 AND characteristic_key = ANY($3)`,
			q.to, q.from, keys); err != nil {
			return err
		}
	}
	batch := &pgx.Batch{}
	for _, f := range facts {
		if err := insertFact(ctx, tx, f); err != nil {
			return err
		}
		for _, c := range f.Contributions {
			batch.Queue(`UPDATE evidence SET contribution = $2 WHERE id = $1`, c.EvidenceID, c.Value)
		}
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

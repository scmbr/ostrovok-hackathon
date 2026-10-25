package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// saveFacts заменяет активные факты и свидетельства характеристики key у отеля и его фасадов: прежние получают superseded.
// Свидетельства пишутся первыми, факт ссылается на них через evidence_ids.
func saveFacts(ctx context.Context, pool *pgxpool.Pool, hotelID int64, key string, facts []domain.Fact) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	const ofHotel = `characteristic_key = $4
  AND ((entity_type = $5 AND entity_id = ($1::bigint)::text)
    OR (entity_type = $6 AND entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $1::bigint)))`
	if _, err := tx.Exec(ctx, `UPDATE facts SET status = $2 WHERE status = $3 AND `+ofHotel,
		hotelID, domain.FactSuperseded, domain.FactActive, key, domain.EntityHotel, domain.EntityFacade); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE evidence SET status = $2 WHERE status = $3 AND `+ofHotel,
		hotelID, domain.FactSuperseded, domain.FactActive, key, domain.EntityHotel, domain.EntityFacade); err != nil {
		return err
	}
	for _, f := range facts {
		ids := make([]int64, 0, len(f.Evidence))
		for _, e := range f.Evidence {
			claimed, err := json.Marshal(e.Claimed)
			if err != nil {
				return err
			}
			details, err := json.Marshal(e.Details)
			if err != nil {
				return err
			}
			var id int64
			if err := tx.QueryRow(ctx, `
INSERT INTO evidence (entity_type, entity_id, characteristic_key, claimed_value, source, method,
                      author_type, author_id, reliability, contribution, observed_at, valid_until, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id`,
				e.EntityType, e.EntityID, e.Key, claimed, e.Source, e.Method,
				e.AuthorType, e.AuthorID, e.Reliability, e.Contribution(), e.ObservedAt, e.ValidUntil, details).Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		value, err := json.Marshal(f.Value)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO facts (entity_type, entity_id, characteristic_key, value, source, method,
                   confidence, observed_at, valid_until, evidence_ids, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			f.EntityType, f.EntityID, f.Key, value, f.Source, f.Method,
			f.Confidence, f.ObservedAt, f.ValidUntil, ids, domain.FactActive); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

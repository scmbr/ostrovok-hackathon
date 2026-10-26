package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
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
	if _, err := tx.Exec(ctx, `UPDATE conflicts SET status = $2 WHERE status = $3 AND `+ofHotel,
		hotelID, domain.ConflictSuperseded, domain.ConflictOpen, key, domain.EntityHotel, domain.EntityFacade); err != nil {
		return err
	}
	for _, f := range facts {
		if err := insertFact(ctx, tx, f); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// insertFact пишет свидетельства факта, затем сам факт со ссылками на них и, если есть, конфликт.
func insertFact(ctx context.Context, tx pgx.Tx, f domain.Fact) error {
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
	var factID int64
	if err := tx.QueryRow(ctx, `
INSERT INTO facts (entity_type, entity_id, characteristic_key, value, source, method,
                   confidence, observed_at, valid_until, evidence_ids, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id`,
		f.EntityType, f.EntityID, f.Key, value, f.Source, f.Method,
		f.Confidence, f.ObservedAt, f.ValidUntil, ids, domain.FactActive).Scan(&factID); err != nil {
		return err
	}
	if f.Conflict != nil {
		if _, err := tx.Exec(ctx, `
INSERT INTO conflicts (entity_type, entity_id, characteristic_key, fact_ids, evidence_ids, description, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			f.EntityType, f.EntityID, f.Key, []int64{factID}, ids, f.Conflict.Description, domain.ConflictOpen); err != nil {
			return err
		}
	}
	return nil
}

// saveDistrictFacts заменяет активные факты и свидетельства характеристики key у района (сущность district).
func saveDistrictFacts(ctx context.Context, pool *pgxpool.Pool, district, key string, facts []domain.Fact) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	const ofDistrict = `characteristic_key = $4 AND entity_type = $5 AND entity_id = $1::text`
	for _, q := range []struct{ table, to, from string }{
		{"facts", domain.FactSuperseded, domain.FactActive},
		{"evidence", domain.FactSuperseded, domain.FactActive},
		{"conflicts", domain.ConflictSuperseded, domain.ConflictOpen},
	} {
		if _, err := tx.Exec(ctx, `UPDATE `+q.table+` SET status = $2 WHERE status = $3 AND `+ofDistrict,
			district, q.to, q.from, key, domain.EntityDistrict); err != nil {
			return err
		}
	}
	for _, f := range facts {
		if err := insertFact(ctx, tx, f); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// evidenceScope — какие сущности затрагивает запись: условие по $1 (область) и $2 (характеристика).
// Область — отель (сам отель и его фасады) или район.
type evidenceScope struct {
	arg    any
	clause string
}

func hotelScope(hotelID int64) evidenceScope {
	return evidenceScope{hotelID, `characteristic_key = $2
  AND ((entity_type = 'hotel' AND entity_id = ($1::bigint)::text)
    OR (entity_type = 'facade' AND entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $1::bigint)))`}
}

func districtScope(district string) evidenceScope {
	return evidenceScope{district, `characteristic_key = $2 AND entity_type = 'district' AND entity_id = $1::text`}
}

// saveEvidence записывает свидетельства инструмента и пересобирает факты области.
// Снимаются (superseded) только активные свидетельства авторов authors: ответы отельеров и гостей остаются.
// Затем пишутся новые свидетельства, из всех активных свидетельств области собираются факты (domain.AssembleFacts),
// прежние факты и конфликты области получают superseded. Всё в одной транзакции.
// Пустой evs — только снять прежнее свидетельство инструмента (например, маршрут больше не найден).
func saveEvidence(ctx context.Context, pool *pgxpool.Pool, scope evidenceScope, key string,
	authors []domain.Author, evs []domain.Evidence) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден

	types := make([]string, len(authors))
	ids := make([]string, len(authors))
	for i, a := range authors {
		types[i], ids[i] = a.Type, a.ID
	}
	if _, err := tx.Exec(ctx, `
UPDATE evidence SET status = $3
WHERE status = $4 AND `+scope.clause+`
  AND (author_type, author_id) IN (SELECT * FROM unnest($5::text[], $6::text[]))`,
		scope.arg, key, domain.FactSuperseded, domain.FactActive, types, ids); err != nil {
		return err
	}
	for _, e := range evs {
		if err := insertEvidence(ctx, tx, e); err != nil {
			return err
		}
	}

	active, err := loadActiveEvidence(ctx, tx, scope, key)
	if err != nil {
		return err
	}
	for _, q := range []struct{ table, to, from string }{
		{"facts", domain.FactSuperseded, domain.FactActive},
		{"conflicts", domain.ConflictSuperseded, domain.ConflictOpen},
	} {
		if _, err := tx.Exec(ctx, `UPDATE `+q.table+` SET status = $3 WHERE status = $4 AND `+scope.clause,
			scope.arg, key, q.to, q.from); err != nil {
			return err
		}
	}
	for _, f := range domain.AssembleFacts(active) {
		if err := insertFact(ctx, tx, f); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertEvidence(ctx context.Context, tx pgx.Tx, e domain.Evidence) error {
	claimed, err := json.Marshal(e.Claimed)
	if err != nil {
		return err
	}
	details, err := json.Marshal(e.Details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO evidence (entity_type, entity_id, characteristic_key, claimed_value, source, method,
                      author_type, author_id, reliability, contribution, observed_at, valid_until, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		e.EntityType, e.EntityID, e.Key, claimed, e.Source, e.Method,
		e.AuthorType, e.AuthorID, e.Reliability, e.Contribution(), e.ObservedAt, e.ValidUntil, details)
	return err
}

// loadActiveEvidence читает активные свидетельства области в порядке записи.
func loadActiveEvidence(ctx context.Context, tx pgx.Tx, scope evidenceScope, key string) ([]domain.Evidence, error) {
	rows, err := tx.Query(ctx, `
SELECT id, entity_type, entity_id, characteristic_key, claimed_value, source, method,
       author_type, author_id, reliability, observed_at, valid_until, details
FROM evidence WHERE status = $3 AND `+scope.clause+` ORDER BY id`, scope.arg, key, domain.FactActive)
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

// insertFact пишет факт со ссылками на свидетельства и, если есть, конфликт.
func insertFact(ctx context.Context, tx pgx.Tx, f domain.Fact) error {
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
		f.Confidence, f.ObservedAt, f.ValidUntil, f.EvidenceIDs, domain.FactActive).Scan(&factID); err != nil {
		return err
	}
	if f.Conflict != nil {
		if _, err := tx.Exec(ctx, `
INSERT INTO conflicts (entity_type, entity_id, characteristic_key, fact_ids, evidence_ids, description, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			f.EntityType, f.EntityID, f.Key, []int64{factID}, f.EvidenceIDs, f.Conflict.Description, domain.ConflictOpen); err != nil {
			return err
		}
	}
	return nil
}

// evidenceWriter — запись свидетельств инструментов; встраивается в хранилища инструментов.
type evidenceWriter struct{ pool *pgxpool.Pool }

// SaveHotelEvidence записывает свидетельства характеристики key у отеля и его фасадов, см. saveEvidence.
func (w evidenceWriter) SaveHotelEvidence(ctx context.Context, hotelID int64, key string,
	authors []domain.Author, evs []domain.Evidence) error {
	return saveEvidence(ctx, w.pool, hotelScope(hotelID), key, authors, evs)
}

// SaveDistrictEvidence записывает свидетельства характеристики key у района, см. saveEvidence.
func (w evidenceWriter) SaveDistrictEvidence(ctx context.Context, district, key string,
	authors []domain.Author, evs []domain.Evidence) error {
	return saveEvidence(ctx, w.pool, districtScope(district), key, authors, evs)
}

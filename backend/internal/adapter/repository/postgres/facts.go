package postgres

import (
	"context"
	"encoding/json"
	"time"

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
// Затем пишутся новые свидетельства, из всех активных свидетельств области движок правды собирает факты
// (domain.AssembleFacts) и вклады свидетельств, прежние факты и конфликты области получают superseded.
// Всё в одной транзакции.
// Записанным свидетельствам evs проставляются id.
// Пустой evs — только снять прежнее свидетельство инструмента (например, маршрут больше не найден).
func saveEvidence(ctx context.Context, pool *pgxpool.Pool, rules map[string]domain.TruthParams, scope evidenceScope, key string,
	authors []domain.Author, evs []domain.Evidence) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if err := saveEvidenceTx(ctx, tx, rules, scope, key, authors, evs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// saveEvidenceTx — saveEvidence в чужой транзакции: вызывающий сам фиксирует или откатывает её.
func saveEvidenceTx(ctx context.Context, tx pgx.Tx, rules map[string]domain.TruthParams, scope evidenceScope, key string,
	authors []domain.Author, evs []domain.Evidence) error {
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
	for i := range evs {
		id, err := insertEvidence(ctx, tx, evs[i])
		if err != nil {
			return err
		}
		evs[i].ID = id // вызывающему нужны id записанных свидетельств (ссылки из room_placements)
	}

	active, err := loadActiveEvidence(ctx, tx, scope, key)
	if err != nil {
		return err
	}
	prevConflicts, err := openConflicts(ctx, tx, scope, key)
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
	trust, err := loadTrust(ctx, tx)
	if err != nil {
		return err
	}
	tc := domain.TruthContext{Now: time.Now(), Trust: trust, Rules: rules}
	facts := domain.AssembleFacts(active, tc)
	for _, f := range facts {
		if err := insertFact(ctx, tx, f); err != nil {
			return err
		}
		if err := updateContributions(ctx, tx, f.Contributions); err != nil {
			return err
		}
	}
	return recordResolutions(ctx, tx, prevConflicts, facts, active, tc)
}

// openConflict — открытый конфликт сущности до пересборки фактов.
type openConflict struct {
	id                        int64
	entityType, entityID, key string
}

func openConflicts(ctx context.Context, tx pgx.Tx, scope evidenceScope, key string) ([]openConflict, error) {
	rows, err := tx.Query(ctx, `SELECT id, entity_type, entity_id, characteristic_key FROM conflicts WHERE status = $3 AND `+scope.clause,
		scope.arg, key, domain.ConflictOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []openConflict
	for rows.Next() {
		var c openConflict
		if err := rows.Scan(&c.id, &c.entityType, &c.entityID, &c.key); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// recordResolutions пишет причину в прежние конфликты, которые после пересборки не открылись снова: стороны сошлись,
// вес одной стороны упал до нуля, расхождение стало слабее порога или свидетельства сняты (domain.ExplainClosedConflict).
// Снятые из-за пересборки и тут же открытые заново конфликты причины не получают.
func recordResolutions(ctx context.Context, tx pgx.Tx, prev []openConflict, facts []domain.Fact, active []domain.Evidence, tc domain.TruthContext) error {
	for _, c := range prev {
		var f *domain.Fact
		for i := range facts {
			if facts[i].EntityType == c.entityType && facts[i].EntityID == c.entityID && facts[i].Key == c.key {
				f = &facts[i]
			}
		}
		var res domain.ConflictResolution
		switch {
		case f == nil:
			res = domain.ConflictResolution{Kind: domain.ResolutionEvidenceRemoved, Text: "свидетельства сняты: факта больше нет"}
		case f.Conflict != nil:
			continue // конфликт открылся заново, это тот же спор
		default:
			var es []domain.Evidence
			for _, e := range active {
				if e.EntityType == c.entityType && e.EntityID == c.entityID {
					es = append(es, e)
				}
			}
			res = domain.ExplainClosedConflict(*f, es, tc)
		}
		if _, err := tx.Exec(ctx, `UPDATE conflicts SET resolution = $2, resolution_kind = $3 WHERE id = $1`, c.id, res.Text, res.Kind); err != nil {
			return err
		}
	}
	return nil
}

func insertEvidence(ctx context.Context, tx pgx.Tx, e domain.Evidence) (int64, error) {
	claimed, err := json.Marshal(e.Claimed)
	if err != nil {
		return 0, err
	}
	details, err := json.Marshal(e.Details)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `
INSERT INTO evidence (entity_type, entity_id, characteristic_key, claimed_value, source, method,
                      author_type, author_id, reliability, contribution, observed_at, valid_until, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id`,
		e.EntityType, e.EntityID, e.Key, claimed, e.Source, e.Method,
		e.AuthorType, e.AuthorID, e.Reliability, e.Contribution(), e.ObservedAt, e.ValidUntil, details).Scan(&id)
	return id, err
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
                   confidence, observed_at, valid_until, evidence_ids, status, weak_disagreement, stale)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id`,
		f.EntityType, f.EntityID, f.Key, value, f.Source, f.Method,
		f.Confidence, f.ObservedAt, f.ValidUntil, f.EvidenceIDs, domain.FactActive,
		f.WeakDisagreement, f.Stale).Scan(&factID); err != nil {
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

// updateContributions пишет вклады свидетельств, посчитанные движком правды (evidence.contribution).
func updateContributions(ctx context.Context, tx pgx.Tx, cs []domain.Contribution) error {
	for _, c := range cs {
		if _, err := tx.Exec(ctx, `UPDATE evidence SET contribution = $2 WHERE id = $1`, c.EvidenceID, c.Value); err != nil {
			return err
		}
	}
	return nil
}

// loadTrust читает репутацию авторов-людей (author_trust); нет записи — domain.DefaultTrust.
func loadTrust(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[domain.Author]float64, error) {
	rows, err := q.Query(ctx, `SELECT author_type, author_id, trust FROM author_trust`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.Author]float64{}
	for rows.Next() {
		var a domain.Author
		var trust float64
		if err := rows.Scan(&a.Type, &a.ID, &trust); err != nil {
			return nil, err
		}
		out[a] = trust
	}
	return out, rows.Err()
}

// evidenceWriter — запись свидетельств инструментов; встраивается в хранилища инструментов.
type evidenceWriter struct {
	pool *pgxpool.Pool
	// rules — правила движка правды по характеристикам (реестр).
	rules map[string]domain.TruthParams
	// afterHotel вызывается после записи свидетельств отеля: категории номеров пересобирают свои свидетельства.
	afterHotel HotelSaved
}

// HotelSaved — реакция на запись свидетельств характеристики key у отеля и его фасадов.
type HotelSaved func(ctx context.Context, hotelID int64, key string) error

// SetAfterHotelSave задаёт реакцию на запись свидетельств отеля; ошибка реакции возвращается из SaveHotelEvidence.
func (w *evidenceWriter) SetAfterHotelSave(fn HotelSaved) { w.afterHotel = fn }

// SaveHotelEvidence записывает свидетельства характеристики key у отеля и его фасадов, см. saveEvidence.
func (w evidenceWriter) SaveHotelEvidence(ctx context.Context, hotelID int64, key string,
	authors []domain.Author, evs []domain.Evidence) error {
	if err := saveEvidence(ctx, w.pool, w.rules, hotelScope(hotelID), key, authors, evs); err != nil {
		return err
	}
	if w.afterHotel != nil {
		return w.afterHotel(ctx, hotelID, key)
	}
	return nil
}

// SaveDistrictEvidence записывает свидетельства характеристики key у района, см. saveEvidence.
func (w evidenceWriter) SaveDistrictEvidence(ctx context.Context, district, key string,
	authors []domain.Author, evs []domain.Evidence) error {
	return saveEvidence(ctx, w.pool, w.rules, districtScope(district), key, authors, evs)
}

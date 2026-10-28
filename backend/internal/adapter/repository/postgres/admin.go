package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/admin"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// AdminStore читает журнал агентов и конфликты для экрана «под капотом» (usecase/admin).
type AdminStore struct{ pool *pgxpool.Pool }

func NewAdminStore(pool *pgxpool.Pool) *AdminStore { return &AdminStore{pool: pool} }

// hotelOfEntity — SQL-выражение: отель, которому принадлежит сущность (alias — таблица с entity_type и entity_id).
func hotelOfEntity(alias string) string {
	return `CASE ` + alias + `.entity_type
  WHEN 'hotel' THEN ` + alias + `.entity_id::bigint
  WHEN 'facade' THEN (SELECT hotel_id FROM facades WHERE id = ` + alias + `.entity_id::bigint)
  WHEN 'room_category' THEN (SELECT hotel_id FROM room_categories WHERE id = ` + alias + `.entity_id::bigint)
END`
}

// Runs — прогоны агентов: группа записей журнала по session_id, новые первыми.
func (s *AdminStore) Runs(ctx context.Context, agentName string, hotelID int64, limit int) ([]admin.Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
SELECT agent, session_id, COALESCE(max(hotel_id), 0), (array_agg(mode ORDER BY step DESC, id DESC))[1],
       count(*), count(*) FILTER (WHERE decision LIKE 'отказ%'), min(created_at), max(created_at),
       (array_agg(decision ORDER BY step, id))[1]
FROM agent_trace
WHERE ($1 = '' OR agent = $1) AND ($2::bigint = 0 OR hotel_id = $2)
GROUP BY agent, session_id
ORDER BY min(created_at) DESC, session_id LIMIT $3`, agentName, hotelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.Run
	for rows.Next() {
		var r admin.Run
		if err := rows.Scan(&r.Agent, &r.SessionID, &r.HotelID, &r.Mode, &r.Steps, &r.Refusals, &r.StartedAt, &r.EndedAt, &r.Title); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunSteps — записи одного прогона по порядку.
func (s *AdminStore) RunSteps(ctx context.Context, sessionID string) ([]agent.TraceStep, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, agent, session_id, COALESCE(hotel_id, 0), mode, step, decision, COALESCE(tool, ''), input, output, created_at
FROM agent_trace WHERE session_id = $1 ORDER BY step, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agent.TraceStep
	for rows.Next() {
		var t agent.TraceStep
		var in, o []byte
		if err := rows.Scan(&t.ID, &t.Agent, &t.SessionID, &t.HotelID, &t.Mode, &t.Step, &t.Decision, &t.Tool, &in, &o, &t.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(in, &t.Input)
		_ = json.Unmarshal(o, &t.Output)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Conflicts — открытые, закрытые (с причиной закрытия) или все конфликты.
func (s *AdminStore) Conflicts(ctx context.Context, f admin.ConflictFilter) ([]admin.Conflict, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var cond string
	switch f.Status {
	case "closed":
		cond = `c.status = 'superseded' AND c.resolution_kind IS NOT NULL`
	case "all":
		cond = `(c.status = 'open' OR c.resolution_kind IS NOT NULL)`
	default:
		cond = `c.status = 'open'`
	}
	rows, err := s.pool.Query(ctx, `
SELECT * FROM (
  SELECT c.id, c.entity_type, c.entity_id, c.characteristic_key, COALESCE(`+hotelOfEntity("c")+`, 0) AS hotel_id, c.status,
         COALESCE(c.description, ''), COALESCE(c.resolution, ''), COALESCE(c.resolution_kind, ''), c.detected_at
  FROM conflicts c WHERE `+cond+`) x
WHERE ($1::bigint = 0 OR hotel_id = $1) ORDER BY id DESC LIMIT $2`, f.HotelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.Conflict
	for rows.Next() {
		var c admin.Conflict
		if err := rows.Scan(&c.ID, &c.EntityType, &c.EntityID, &c.Key, &c.HotelID, &c.Status, &c.Description, &c.Resolution, &c.ResolutionKind, &c.DetectedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Question — последний вопрос отельеру по характеристике (и категории номеров, если entityID задан).
func (s *AdminStore) Question(ctx context.Context, hotelID int64, key, entityID string) (admin.QuestionRef, bool, error) {
	var q admin.QuestionRef
	err := s.pool.QueryRow(ctx, `
SELECT id, status, COALESCE(format, '') FROM hotelier_questions
WHERE hotel_id = $1 AND characteristic_key = $2 AND ($3 = '' OR COALESCE(entity_id, '') = $3)
ORDER BY id DESC LIMIT 1`, hotelID, key, entityID).Scan(&q.ID, &q.Status, &q.Format)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, false, nil
	}
	return q, err == nil, err
}

// Tasks — задачи пересчёта (сигнал гостей), новые первыми.
func (s *AdminStore) Tasks(ctx context.Context, hotelID int64, limit int) ([]admin.Task, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, hotel_id, characteristic_key, reason, status, COALESCE(outcome, ''), created_at, done_at
FROM recompute_tasks WHERE ($1::bigint = 0 OR hotel_id = $1) ORDER BY id DESC LIMIT $2`, hotelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.Task
	for rows.Next() {
		var t admin.Task
		if err := rows.Scan(&t.ID, &t.HotelID, &t.Key, &t.Reason, &t.Status, &t.Outcome, &t.CreatedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// StaleFacts — активные факты, поставленные на пересчёт.
func (s *AdminStore) StaleFacts(ctx context.Context, hotelID int64, limit int) ([]admin.StaleFact, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
SELECT * FROM (SELECT f.entity_type, f.entity_id, f.characteristic_key, COALESCE(`+hotelOfEntity("f")+`, 0) AS hotel_id, f.valid_until
FROM facts f WHERE f.status = $1 AND f.stale) x WHERE ($2::bigint = 0 OR hotel_id = $2) LIMIT $3`, domain.FactActive, hotelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.StaleFact
	for rows.Next() {
		var f admin.StaleFact
		if err := rows.Scan(&f.EntityType, &f.EntityID, &f.Key, &f.HotelID, &f.ValidUntil); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Summary — покрытие характеристик у демо-отелей, число конфликтов и вопросов отельерам.
func (s *AdminStore) Summary(ctx context.Context) (admin.Summary, error) {
	var sm admin.Summary
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM hotels WHERE demo`).Scan(&sm.DemoHotels); err != nil {
		return sm, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT characteristic_key, count(*), avg(confidence), count(*) FILTER (WHERE confidence < $2)
FROM facts WHERE status = $1 AND entity_type = 'hotel' AND entity_id IN (SELECT id::text FROM hotels WHERE demo)
GROUP BY characteristic_key ORDER BY characteristic_key`, domain.FactActive, domain.ConfidentFact)
	if err != nil {
		return sm, err
	}
	for rows.Next() {
		var c admin.Coverage
		if err := rows.Scan(&c.Key, &c.Hotels, &c.AvgConfidence, &c.LowConfidence); err != nil {
			rows.Close()
			return sm, err
		}
		sm.Coverage = append(sm.Coverage, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sm, err
	}
	if err := s.pool.QueryRow(ctx, `
SELECT (SELECT count(*) FROM conflicts WHERE status = 'open'),
       (SELECT count(*) FROM facts WHERE status = $1 AND weak_disagreement),
       (SELECT count(*) FROM facts WHERE status = $1 AND stale)`, domain.FactActive).Scan(&sm.OpenConflicts, &sm.WeakFacts, &sm.StaleFacts); err != nil {
		return sm, err
	}
	sm.Questions = map[string]int{}
	qrows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM hotelier_questions GROUP BY status`)
	if err != nil {
		return sm, err
	}
	defer qrows.Close()
	for qrows.Next() {
		var st string
		var n int
		if err := qrows.Scan(&st, &n); err != nil {
			return sm, err
		}
		sm.Questions[st] = n
	}
	if err := qrows.Err(); err != nil {
		return sm, err
	}
	qrows.Close()
	err = s.pool.QueryRow(ctx, `SELECT count(DISTINCT hotel_id) FROM hotelier_questions WHERE hotel_id IN (SELECT id FROM hotels WHERE demo)`).Scan(&sm.HotelsAsked)
	return sm, err
}

// UnmetWishes — желания гостей, которых нет в реестре: частые первыми, затем свежие.
func (s *AdminStore) UnmetWishes(ctx context.Context, regionID int64, limit int) ([]admin.UnmetWish, error) {
	rows, err := s.pool.Query(ctx, `
SELECT text, normalized, count, first_seen, last_seen, proposal
FROM unmet_wishes WHERE region_id = $1
ORDER BY count DESC, last_seen DESC, id LIMIT $2`, regionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.UnmetWish
	for rows.Next() {
		var w admin.UnmetWish
		if err := rows.Scan(&w.Text, &w.Normalized, &w.Count, &w.FirstSeen, &w.LastSeen, &w.Proposal); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

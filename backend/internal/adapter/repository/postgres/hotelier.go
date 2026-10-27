package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
)

// HotelierStore — хранилище агента-интервьюера: вопросы, спрос, репутация, скрытая истина мока и свидетельства отельера.
type HotelierStore struct {
	pool  *pgxpool.Pool
	rules map[string]domain.TruthParams
	agent *AgentStore
}

func NewHotelierStore(pool *pgxpool.Pool, rules map[string]domain.TruthParams) *HotelierStore {
	return &HotelierStore{pool: pool, rules: rules, agent: NewAgentStore(pool, rules)}
}

func (s *HotelierStore) Hotel(ctx context.Context, id int64) (hotelier.Hotel, error) {
	var h hotelier.Hotel
	err := s.pool.QueryRow(ctx, `
SELECT id, name, COALESCE(district, ''), region_id, COALESCE(hotelier_persona, '') FROM hotels WHERE id = $1`, id).
		Scan(&h.ID, &h.Name, &h.District, &h.RegionID, &h.Persona)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, fmt.Errorf("%w: отель %d", hotelier.ErrNotFound, id)
	}
	return h, err
}

func (s *HotelierStore) HotelFacts(ctx context.Context, hotelID int64) ([]domain.Fact, error) {
	return s.agent.HotelFacts(ctx, hotelID)
}

func (s *HotelierStore) AppendTrace(ctx context.Context, t agent.TraceStep) error {
	return s.agent.AppendTrace(ctx, t)
}

func (s *HotelierStore) Demand(ctx context.Context, regionID int64) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT characteristic_key, sum(count)::int FROM characteristic_demand WHERE region_id = $1 GROUP BY characteristic_key`, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

func (s *HotelierStore) Trust(ctx context.Context, a domain.Author) (float64, error) {
	var t float64
	err := s.pool.QueryRow(ctx, `SELECT trust FROM author_trust WHERE author_type = $1 AND author_id = $2`, a.Type, a.ID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultTrust, nil
	}
	return t, err
}

func (s *HotelierStore) Truth(ctx context.Context, hotelID int64, key string) (map[string]any, bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM mock_hotel_truth WHERE hotel_id = $1 AND characteristic_key = $2`, hotelID, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, err
	}
	return v, true, nil
}

const questionColumns = `id, hotel_id, characteristic_key, question, COALESCE(reason, ''), status, COALESCE(format, ''),
       COALESCE(persona, ''), score, COALESCE(answer_text, ''), parsed, sent_at, asked_at,
       COALESCE(entity_type, ''), COALESCE(entity_id, '')`

func scanQuestion(row pgx.Row) (hotelier.Question, error) {
	var q hotelier.Question
	var score, parsed []byte
	if err := row.Scan(&q.ID, &q.HotelID, &q.Key, &q.Text, &q.Reason, &q.Status, &q.Format, &q.Persona,
		&score, &q.AnswerText, &parsed, &q.SentAt, &q.AskedAt, &q.EntityType, &q.EntityID); err != nil {
		return q, err
	}
	if err := json.Unmarshal(score, &q.Score); err != nil {
		return q, err
	}
	if parsed != nil {
		if err := json.Unmarshal(parsed, &q.Parsed); err != nil {
			return q, err
		}
	}
	return q, nil
}

func (s *HotelierStore) Questions(ctx context.Context, hotelID int64) ([]hotelier.Question, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+questionColumns+` FROM hotelier_questions WHERE hotel_id = $1 ORDER BY id`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hotelier.Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Question — вопрос по id.
func (s *HotelierStore) Question(ctx context.Context, id int64) (hotelier.Question, error) {
	q, err := scanQuestion(s.pool.QueryRow(ctx, `SELECT `+questionColumns+` FROM hotelier_questions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return q, fmt.Errorf("%w: вопрос %d", hotelier.ErrNotFound, id)
	}
	return q, err
}

// SentSince считает вопросы недельного бюджета: подтверждения пожеланий из броней (wish_guarantee) в него не входят.
func (s *HotelierStore) SentSince(ctx context.Context, hotelID int64, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM hotelier_questions WHERE hotel_id = $1 AND sent_at >= $2 AND characteristic_key <> $3`,
		hotelID, since, domain.WishGuaranteeKey).Scan(&n)
	return n, err
}

// HotelList — демо-отели для переключателя ролей.
func (s *HotelierStore) HotelList(ctx context.Context) ([]hotelier.HotelSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, COALESCE(district, ''), COALESCE(hotelier_persona, '') FROM hotels WHERE demo ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hotelier.HotelSummary
	for rows.Next() {
		var h hotelier.HotelSummary
		if err := rows.Scan(&h.ID, &h.Name, &h.District, &h.Persona); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// WishRequests — брони с пожеланием, на которые ждёт ответа отельер: вопрос wish_guarantee ещё открыт.
func (s *HotelierStore) WishRequests(ctx context.Context, hotelID int64) ([]hotelier.WishRequest, error) {
	rows, err := s.pool.Query(ctx, `
SELECT b.wish_question_id, b.id, COALESCE(rc.name, ''), b.wishes, b.created_at
FROM bookings b
JOIN hotelier_questions q ON q.id = b.wish_question_id AND q.status IN ('open', 'needs_clarification')
LEFT JOIN room_categories rc ON rc.id = b.room_category_id
WHERE b.hotel_id = $1 ORDER BY b.id`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hotelier.WishRequest
	for rows.Next() {
		var w hotelier.WishRequest
		var wishes []byte
		if err := rows.Scan(&w.QuestionID, &w.BookingID, &w.Category, &wishes, &w.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(wishes, &w.Wishes); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// LastResolution — причина, записанная движком правды при последнем закрытии конфликта сущности и характеристики.
func (s *HotelierStore) LastResolution(ctx context.Context, entityType, entityID, key string) (domain.ConflictResolution, bool, error) {
	var res domain.ConflictResolution
	var kind, text *string
	err := s.pool.QueryRow(ctx, `
SELECT resolution_kind, resolution FROM conflicts
WHERE entity_type = $1 AND entity_id = $2 AND characteristic_key = $3 AND status = $4 AND resolution_kind IS NOT NULL
ORDER BY id DESC LIMIT 1`, entityType, entityID, key, domain.ConflictSuperseded).Scan(&kind, &text)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, false, nil
	}
	if err != nil {
		return res, false, err
	}
	res.Kind, res.Text = *kind, ""
	if text != nil {
		res.Text = *text
	}
	// Автор, чей вес упал, отдельно не хранится: он назван в тексте причины (domain.ExplainClosedConflict).
	if res.Kind == domain.ResolutionWeightDropped && strings.Contains(res.Text, "(отель)") {
		res.Author = domain.Author{Type: domain.AuthorHotelier}
	}
	return res, true, nil
}

// FactAt читает активный факт по адресу «сущность + характеристика» со свидетельствами и открытым конфликтом.
func (s *HotelierStore) FactAt(ctx context.Context, entityType, entityID, key string) (hotelier.FactSnap, error) {
	var snap hotelier.FactSnap
	var value []byte
	var conflict *string
	var evIDs []int64
	err := s.pool.QueryRow(ctx, `
SELECT f.id, f.value, f.confidence, f.weak_disagreement, f.stale, f.evidence_ids,
       (SELECT c.description FROM conflicts c WHERE f.id = ANY(c.fact_ids) AND c.status = $5 LIMIT 1)
FROM facts f
WHERE f.entity_type = $1 AND f.entity_id = $2 AND f.characteristic_key = $3 AND f.status = $4
ORDER BY f.id DESC LIMIT 1`, entityType, entityID, key, domain.FactActive, domain.ConflictOpen).
		Scan(&snap.FactID, &value, &snap.Confidence, &snap.WeakDisagreement, &snap.Stale, &evIDs, &conflict)
	if errors.Is(err, pgx.ErrNoRows) {
		return snap, nil
	}
	if err != nil {
		return snap, err
	}
	snap.Found = true
	if err := json.Unmarshal(value, &snap.Value); err != nil {
		return snap, err
	}
	if conflict != nil {
		snap.Conflict = *conflict
	}
	views, err := loadEvidenceViews(ctx, s.pool, evIDs)
	if err != nil {
		return snap, err
	}
	for _, e := range views {
		snap.Evidence = append(snap.Evidence, hotelier.EvidenceSnap{ID: e.ID, Source: e.Source, AuthorType: e.AuthorType, AuthorTrust: e.AuthorTrust,
			Claimed: e.Claimed, Reliability: e.Reliability, Contribution: e.Contribution, ObservedAt: e.ObservedAt, Comment: e.Comment})
	}
	return snap, nil
}

// UpsertCandidate пишет кандидата: с q.ID — обновляет копящийся вопрос (ценность, формат, причина), без — вставляет новый.
func (s *HotelierStore) UpsertCandidate(ctx context.Context, q hotelier.Question) (hotelier.Question, error) {
	score, err := json.Marshal(q.Score)
	if err != nil {
		return q, err
	}
	var row pgx.Row
	if q.ID != 0 {
		row = s.pool.QueryRow(ctx, `
UPDATE hotelier_questions SET reason = $2, status = $3, format = $4, value_score = $5, score = $6
WHERE id = $1 AND status IN ('pending', 'pending_low_value')
RETURNING `+questionColumns, q.ID, q.Reason, q.Status, q.Format, q.Score.Value, score)
	} else {
		row = s.pool.QueryRow(ctx, `
INSERT INTO hotelier_questions (hotel_id, characteristic_key, question, reason, status, format, value_score, score, entity_type, entity_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''))
RETURNING `+questionColumns, q.HotelID, q.Key, q.Text, q.Reason, q.Status, q.Format, q.Score.Value, score, q.EntityType, q.EntityID)
	}
	return scanQuestion(row)
}

func (s *HotelierStore) Send(ctx context.Context, id int64, text, format, persona string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE hotelier_questions SET question = $2, format = $3, persona = NULLIF($4, ''), status = 'open', sent_at = $5
WHERE id = $1 AND status IN ('pending', 'pending_low_value')`, id, text, format, persona, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("вопрос уже отправлен или закрыт")
	}
	return nil
}

func (s *HotelierStore) RecordAnswer(ctx context.Context, id int64, status, answerText string, parsed map[string]any, at time.Time) error {
	raw, err := json.Marshal(parsed)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
UPDATE hotelier_questions SET status = $2, answer_text = $3, parsed = $4, answered_at = $5 WHERE id = $1`,
		id, status, answerText, raw, at)
	return err
}

func (s *HotelierStore) Close(ctx context.Context, id int64, note string) error {
	_, err := s.pool.Exec(ctx, `
UPDATE hotelier_questions SET status = 'closed', reason = COALESCE(reason || '; ', '') || $2
WHERE id = $1 AND status IN ('pending', 'pending_low_value')`, id, note)
	return err
}

// SaveAnswerEvidence записывает свидетельство отельера. Прежнее активное свидетельство этого отельера по той же
// характеристике снимается (superseded): новое утверждение того же автора заменяет прежнее, а не добавляется рядом.
// Факт собирает движок правды.
func (s *HotelierStore) SaveAnswerEvidence(ctx context.Context, ev domain.Evidence) error {
	scope, err := answerScope(ev)
	if err != nil {
		return err
	}
	return saveEvidence(ctx, s.pool, s.rules, scope, ev.Key,
		[]domain.Author{{Type: ev.AuthorType, ID: ev.AuthorID}}, []domain.Evidence{ev})
}

// answerScope — область записи: категория номеров или отель вместе с фасадами (автор-отельер привязан к одному отелю).
func answerScope(ev domain.Evidence) (evidenceScope, error) {
	if ev.EntityType == domain.EntityRoomCategory {
		id, err := strconv.ParseInt(ev.EntityID, 10, 64)
		if err != nil {
			return evidenceScope{}, err
		}
		return categoryScope(id), nil
	}
	id, err := strconv.ParseInt(ev.AuthorID, 10, 64)
	if err != nil {
		return evidenceScope{}, err
	}
	return hotelScope(id), nil
}

// WriteAnswerEvidence снимает прежнее свидетельство того же автора и пишет новое, факты не собирает: их соберёт
// RebuildAnswerScope или пересборка по новой репутации, один раз.
func (s *HotelierStore) WriteAnswerEvidence(ctx context.Context, ev domain.Evidence) error {
	scope, err := answerScope(ev)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден
	if err := writeEvidenceTx(ctx, tx, scope, ev.Key, []domain.Author{{Type: ev.AuthorType, ID: ev.AuthorID}}, []domain.Evidence{ev}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RebuildAnswerScope собирает факты области свидетельства из активных свидетельств.
func (s *HotelierStore) RebuildAnswerScope(ctx context.Context, ev domain.Evidence) error {
	scope, err := answerScope(ev)
	if err != nil {
		return err
	}
	return saveEvidence(ctx, s.pool, s.rules, scope, ev.Key, nil, nil)
}

// Reopen возвращает вопрос в open с уточняющим текстом; предыдущий ответ остаётся в answer_text.
func (s *HotelierStore) Reopen(ctx context.Context, id int64, text, previousAnswer string) error {
	_, err := s.pool.Exec(ctx, `
UPDATE hotelier_questions SET question = $2, format = 'open', status = 'open', answer_text = $3, answered_at = NULL
WHERE id = $1`, id, text, previousAnswer)
	return err
}

// CategoryIssues — категории отеля с открытым конфликтом по характеристике: утверждение отеля (название) против
// расчёта. Расчётное значение берётся из свидетельства инструмента, где стоят и размещения категории.
func (s *HotelierStore) CategoryIssues(ctx context.Context, hotelID int64) ([]hotelier.CategoryIssue, error) {
	rs := NewRoomStore(s.pool, s.rules)
	cats, err := rs.Categories(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	facades, err := rs.Facades(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	cfs, err := rs.CategoryFacts(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	var out []hotelier.CategoryIssue
	for _, cf := range cfs {
		if cf.Fact.Conflict == nil {
			continue
		}
		var cat *domain.RoomCategory
		for k := range cats {
			if cats[k].ID == cf.CategoryID {
				cat = &cats[k]
			}
		}
		if cat == nil {
			continue
		}
		is := hotelier.CategoryIssue{CategoryID: cat.ID, Name: cat.Name, Key: cf.Fact.Key, Confidence: cf.Fact.Confidence, Conflict: cf.Fact.Conflict.Description}
		if c, ok := cf.Fact.Value["view"]; ok {
			is.Value = fmt.Sprint(c)
		}
		for _, e := range cf.Evidence {
			if e.AuthorType == domain.AuthorTool {
				if val, ok := e.Claimed["view"]; ok {
					is.CalcValue = fmt.Sprint(val)
				}
			}
		}
		if is.CalcValue == "" {
			continue
		}
		for _, e := range cf.Evidence {
			if val, ok := e.Claimed["view"]; ok && e.AuthorType == domain.AuthorGuest && fmt.Sprint(val) != is.CalcValue {
				is.GuestAgainst++
			}
		}
		for _, p := range cat.Placements {
			label := "фасад"
			for _, f := range facades {
				if f.ID == p.FacadeID {
					label = "фасад на " + hotelier.Compass(f.AzimuthDeg)
					if f.Courtyard || !f.FacesStreet {
						label += ", во двор"
					}
				}
			}
			is.Placements = append(is.Placements, hotelier.IssuePlacement{Facade: label, From: p.FloorFrom, To: p.FloorTo, Rooms: p.Rooms})
		}
		out = append(out, is)
	}
	return out, nil
}

// ResetCounts — сколько записей снял hotelier-reset.
type ResetCounts struct {
	Questions, Evidence, Reactivated, Trust, Trace int
}

// ResetHotelier возвращает отельеров к состоянию до интервью: удаляет вопросы, журнал интервьюера, свидетельства из
// ответов (метод hotelier_answer@1) и репутацию авторов. Утверждения, которые ответ заменил (название «Sea View» из
// мока), снова становятся активными. Факты и конфликты по затронутым характеристикам снимаются: пересобирает их
// движок правды (collect truth, как делает команда hotelier-reset).
func (s *HotelierStore) ResetHotelier(ctx context.Context) (ResetCounts, error) {
	var rc ResetCounts
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return rc, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат безвреден

	tag, err := tx.Exec(ctx, `DELETE FROM hotelier_questions`)
	if err != nil {
		return rc, err
	}
	rc.Questions = int(tag.RowsAffected())
	if tag, err = tx.Exec(ctx, `DELETE FROM agent_trace WHERE agent = $1`, hotelier.AgentName); err != nil {
		return rc, err
	}
	rc.Trace = int(tag.RowsAffected())

	rows, err := tx.Query(ctx, `DELETE FROM evidence WHERE author_type = $1 AND method = $2 RETURNING characteristic_key`,
		domain.AuthorHotelier, hotelier.HotelierMethod)
	if err != nil {
		return rc, err
	}
	keys := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return rc, err
		}
		keys[k] = true
		rc.Evidence++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rc, err
	}
	if tag, err = tx.Exec(ctx, `
UPDATE evidence e SET status = $1
WHERE e.id IN (SELECT max(id) FROM evidence WHERE author_type = $2 AND status = $3
               GROUP BY entity_type, entity_id, characteristic_key, author_id)
  AND NOT EXISTS (SELECT 1 FROM evidence a WHERE a.status = $1 AND a.author_type = e.author_type AND a.author_id = e.author_id
                  AND a.entity_type = e.entity_type AND a.entity_id = e.entity_id AND a.characteristic_key = e.characteristic_key)`,
		domain.FactActive, domain.AuthorHotelier, domain.FactSuperseded); err != nil {
		return rc, err
	}
	rc.Reactivated = int(tag.RowsAffected())
	if tag, err = tx.Exec(ctx, `DELETE FROM author_trust`); err != nil {
		return rc, err
	}
	rc.Trust = int(tag.RowsAffected())

	var list []string
	for k := range keys {
		list = append(list, k)
	}
	for _, q := range []struct{ table, to, from string }{
		{"facts", domain.FactSuperseded, domain.FactActive},
		{"conflicts", domain.ConflictSuperseded, domain.ConflictOpen},
	} {
		if _, err := tx.Exec(ctx, `UPDATE `+q.table+` SET status = $1 WHERE status = $2 AND characteristic_key = ANY($3)`, q.to, q.from, list); err != nil {
			return rc, err
		}
	}
	return rc, tx.Commit(ctx)
}

// Fact читает активный факт по id и отель, которому он принадлежит (сам отель, его фасад или категория номеров).
func (s *HotelierStore) Fact(ctx context.Context, id int64) (hotelier.FactRef, error) {
	var ref hotelier.FactRef
	var value []byte
	f := &ref.Fact
	err := s.pool.QueryRow(ctx, `
SELECT f.id, f.entity_type, f.entity_id, f.characteristic_key, f.value, f.source, f.method, f.confidence,
       f.observed_at, f.valid_until, f.weak_disagreement, f.stale,
       CASE f.entity_type
            WHEN 'hotel' THEN f.entity_id::bigint
            WHEN 'facade' THEN (SELECT hotel_id FROM facades WHERE id = f.entity_id::bigint)
            WHEN 'room_category' THEN (SELECT hotel_id FROM room_categories WHERE id = f.entity_id::bigint)
       END
FROM facts f WHERE f.id = $1 AND f.status = $2`, id, domain.FactActive).
		Scan(&f.ID, &f.EntityType, &f.EntityID, &f.Key, &value, &f.Source, &f.Method, &f.Confidence,
			&f.ObservedAt, &f.ValidUntil, &f.WeakDisagreement, &f.Stale, &ref.HotelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ref, fmt.Errorf("%w: факт %d", hotelier.ErrNotFound, id)
	}
	if err != nil {
		// район и прочие сущности без отеля: NULL в hotel_id
		return ref, fmt.Errorf("%w: у факта %d нет отеля, отельер его не правит (%v)", hotelier.ErrNotFound, id, err)
	}
	return ref, json.Unmarshal(value, &f.Value)
}

// MarkStale ставит факт на пересчёт; следующая пересборка движком правды определит stale заново.
func (s *HotelierStore) MarkStale(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE facts SET stale = true WHERE id = $1 AND status = $2`, id, domain.FactActive)
	return err
}

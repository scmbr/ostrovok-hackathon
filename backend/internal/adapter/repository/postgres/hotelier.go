package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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
		return h, errors.New("отель не найден")
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
	rows, err := s.pool.Query(ctx, `SELECT characteristic_key, count FROM characteristic_demand WHERE region_id = $1`, regionID)
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
       COALESCE(persona, ''), score, COALESCE(answer_text, ''), parsed, sent_at, asked_at`

func scanQuestion(row pgx.Row) (hotelier.Question, error) {
	var q hotelier.Question
	var score, parsed []byte
	if err := row.Scan(&q.ID, &q.HotelID, &q.Key, &q.Text, &q.Reason, &q.Status, &q.Format, &q.Persona,
		&score, &q.AnswerText, &parsed, &q.SentAt, &q.AskedAt); err != nil {
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
		return q, errors.New("вопрос не найден")
	}
	return q, err
}

func (s *HotelierStore) SentSince(ctx context.Context, hotelID int64, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM hotelier_questions WHERE hotel_id = $1 AND sent_at >= $2`, hotelID, since).Scan(&n)
	return n, err
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
INSERT INTO hotelier_questions (hotel_id, characteristic_key, question, reason, status, format, value_score, score)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING `+questionColumns, q.HotelID, q.Key, q.Text, q.Reason, q.Status, q.Format, q.Score.Value, score)
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
	hotelID, err := strconv.ParseInt(ev.EntityID, 10, 64)
	if err != nil {
		return err
	}
	return saveEvidence(ctx, s.pool, s.rules, hotelScope(hotelID), ev.Key,
		[]domain.Author{{Type: ev.AuthorType, ID: ev.AuthorID}}, []domain.Evidence{ev})
}

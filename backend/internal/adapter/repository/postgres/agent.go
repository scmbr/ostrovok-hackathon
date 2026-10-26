package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// AgentStore — хранилище агента-сборщика: факты отеля на чтение, пересчёт движком правды, вопросы и журнал.
// Записать значение факта через него нельзя.
type AgentStore struct {
	pool  *pgxpool.Pool
	rules map[string]domain.TruthParams
}

func NewAgentStore(pool *pgxpool.Pool, rules map[string]domain.TruthParams) *AgentStore {
	return &AgentStore{pool: pool, rules: rules}
}

func (s *AgentStore) Hotel(ctx context.Context, id int64) (agent.HotelInfo, error) {
	h := agent.HotelInfo{ID: id}
	err := s.pool.QueryRow(ctx, `SELECT name, COALESCE(district, ''), demo FROM hotels WHERE id = $1`, id).
		Scan(&h.Name, &h.District, &h.Demo)
	if err == pgx.ErrNoRows {
		return h, fmt.Errorf("отель %d не найден", id)
	}
	return h, err
}

// HotelFacts читает активные факты отеля, его фасадов и района вместе с открытым конфликтом.
func (s *AgentStore) HotelFacts(ctx context.Context, hotelID int64) ([]domain.Fact, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.entity_type, f.entity_id, f.characteristic_key, f.value, f.source, f.method, f.confidence,
       f.observed_at, f.valid_until, f.weak_disagreement, f.stale, c.description
FROM facts f
LEFT JOIN conflicts c ON f.id = ANY(c.fact_ids) AND c.status = $2
WHERE f.status = $3 AND (
      (f.entity_type = 'hotel' AND f.entity_id = ($1::bigint)::text)
   OR (f.entity_type = 'facade' AND f.entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $1::bigint))
   OR (f.entity_type = 'district' AND f.entity_id = (SELECT district FROM hotels WHERE id = $1::bigint)))
ORDER BY f.id`, hotelID, domain.ConflictOpen, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Fact
	for rows.Next() {
		var f domain.Fact
		var value []byte
		var conflict *string
		if err := rows.Scan(&f.ID, &f.EntityType, &f.EntityID, &f.Key, &value, &f.Source, &f.Method, &f.Confidence,
			&f.ObservedAt, &f.ValidUntil, &f.WeakDisagreement, &f.Stale, &conflict); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(value, &f.Value); err != nil {
			return nil, err
		}
		if conflict != nil {
			f.Conflict = &domain.Conflict{Description: *conflict}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RecomputeFact пересобирает факты характеристики у отеля, фасадов и района из активных свидетельств:
// запись без новых свидетельств и без снятия старых.
func (s *AgentStore) RecomputeFact(ctx context.Context, hotelID int64, district, key string) error {
	if err := saveEvidence(ctx, s.pool, s.rules, hotelScope(hotelID), key, nil, nil); err != nil {
		return err
	}
	if district == "" {
		return nil
	}
	return saveEvidence(ctx, s.pool, s.rules, districtScope(district), key, nil, nil)
}

// CreateQuestion пишет вопрос отельеру; если такой открытый вопрос уже есть, ничего не меняет.
func (s *AgentStore) CreateQuestion(ctx context.Context, q agent.Question) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
INSERT INTO hotelier_questions (hotel_id, characteristic_key, question, reason, status, value_score)
VALUES ($1, $2, $3, $4, 'open', $5)
ON CONFLICT (hotel_id, characteristic_key) WHERE status IN ('pending', 'pending_low_value', 'open', 'needs_clarification') DO NOTHING`,
		q.HotelID, q.Key, q.Text, q.Reason, q.ValueScore)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// CloseQuestion закрывает открытый вопрос по характеристике и дописывает причину к тексту причины вопроса.
func (s *AgentStore) CloseQuestion(ctx context.Context, hotelID int64, key, note string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
UPDATE hotelier_questions
SET status = 'closed', reason = COALESCE(reason || '; ', '') || 'закрыт: ' || $3
WHERE hotel_id = $1 AND characteristic_key = $2 AND status = 'open'`, hotelID, key, note)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *AgentStore) AppendTrace(ctx context.Context, t agent.TraceStep) error {
	in, err := json.Marshal(t.Input)
	if err != nil {
		return err
	}
	out, err := json.Marshal(t.Output)
	if err != nil {
		return err
	}
	var tool *string
	if t.Tool != "" {
		tool = &t.Tool
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO agent_trace (agent, session_id, hotel_id, mode, step, decision, tool, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.Agent, t.SessionID, t.HotelID, t.Mode, t.Step, t.Decision, tool, in, out)
	return err
}

// ListTrace читает журнал одного прогона по отелю; пустой sessionID — последний прогон.
func (s *AgentStore) ListTrace(ctx context.Context, hotelID int64, sessionID string) ([]agent.TraceStep, error) {
	if sessionID == "" {
		err := s.pool.QueryRow(ctx, `SELECT session_id FROM agent_trace WHERE hotel_id = $1 ORDER BY id DESC LIMIT 1`, hotelID).Scan(&sessionID)
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, agent, session_id, hotel_id, mode, step, decision, COALESCE(tool, ''), input, output, created_at
FROM agent_trace WHERE hotel_id = $1 AND session_id = $2 ORDER BY step, id`, hotelID, sessionID)
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
		if err := json.Unmarshal(in, &t.Input); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(o, &t.Output); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// toolSources — исходные таблицы, без которых инструмент считает неверно или не считает совсем, и команда сбора,
// которая их заполняет. Часть таблиц (граф, части зданий) в демо-сид не входит.
var toolSources = map[string][]struct{ table, collect string }{
	"walk":           {{"walk_edges", "walk-data"}, {"destinations", "walk-data"}},
	"sea-view":       {{"building_parts", "catastro"}},
	"noise-night":    {{"noise_facades", "noise"}},
	"night-services": {{"pois", "pois"}},
}

// RequireSources проверяет, что у инструмента есть исходные данные в этой базе. Иначе пересчёт мог бы ухудшить
// или снять уже собранные свидетельства (например, вид на море без частей зданий кадастра).
func (s *AgentStore) RequireSources(ctx context.Context, tool string) error {
	for _, src := range toolSources[tool] {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+src.table+`)`).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("нет исходных данных для %s: таблица %s пуста (в демо-сид не входит, заполняется командой collect %s); факты не тронуты",
				tool, src.table, src.collect)
		}
	}
	return nil
}

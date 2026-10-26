package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/guests"
)

// GuestStore хранит гостей, брони, анкеты, их свидетельства и задачи пересчёта.
type GuestStore struct {
	pool  *pgxpool.Pool
	rules map[string]domain.TruthParams
	agent *AgentStore
}

func NewGuestStore(pool *pgxpool.Pool, rules map[string]domain.TruthParams) *GuestStore {
	return &GuestStore{pool: pool, rules: rules, agent: NewAgentStore(pool, rules)}
}

func (s *GuestStore) Hotel(ctx context.Context, id int64) (agent.HotelInfo, error) {
	return s.agent.Hotel(ctx, id)
}

func (s *GuestStore) HotelFacts(ctx context.Context, hotelID int64) ([]domain.Fact, error) {
	return s.agent.HotelFacts(ctx, hotelID)
}

func (s *GuestStore) AppendTrace(ctx context.Context, t agent.TraceStep) error {
	return s.agent.AppendTrace(ctx, t)
}

// DemoHotels — демо-отели региона (по flt.HotelID — один); Embellishing — открытый конфликт названия категории.
func (s *GuestStore) DemoHotels(ctx context.Context, flt domain.HotelFilter) ([]guests.Hotel, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, h.name,
       EXISTS (SELECT 1 FROM conflicts k JOIN room_categories c ON k.entity_type = $3 AND k.entity_id = c.id::text
               WHERE c.hotel_id = h.id AND k.characteristic_key = $4 AND k.status = $5
                 AND NOT EXISTS (SELECT 1 FROM evidence e WHERE e.id = ANY(k.evidence_ids) AND e.author_type = 'guest'))
FROM hotels h
WHERE h.demo AND ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2)
ORDER BY h.id`, flt.RegionID, flt.HotelID, domain.EntityRoomCategory, domain.SeaViewKey, domain.ConflictOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []guests.Hotel
	for rows.Next() {
		var h guests.Hotel
		if err := rows.Scan(&h.ID, &h.Name, &h.Embellishing); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *GuestStore) SaveStays(ctx context.Context, stays []guests.Stay) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, st := range stays {
		var guestID int64
		if err := tx.QueryRow(ctx, `
INSERT INTO guests (name, persona, consent) VALUES ($1, $2, '{"survey": true}') RETURNING id`, st.GuestName, st.Persona).Scan(&guestID); err != nil {
			return err
		}
		wishes, err := json.Marshal(st.Wishes)
		if err != nil {
			return err
		}
		exp, err := json.Marshal(map[string]any{"keys": st.Expectations})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO bookings (hotel_id, room_category_id, wishes, expectations, guest_id, facade_id, floor)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, st.HotelID, st.CategoryID, wishes, exp, guestID, st.FacadeID, st.Floor); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ResetGuests удаляет гостей, брони, анкеты, свидетельства и репутацию гостей, задачи пересчёта и журнал сигнала,
// затем пересобирает факты затронутых категорий и фасадов (снимает stale сигнала).
func (s *GuestStore) ResetGuests(ctx context.Context) error {
	type target struct{ id, key string }
	var cats []target
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT entity_id, characteristic_key FROM evidence WHERE author_type = $1 AND entity_type = $2`,
		domain.AuthorGuest, domain.EntityRoomCategory)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.key); err != nil {
			rows.Close()
			return err
		}
		cats = append(cats, t)
	}
	rows.Close()
	type hk struct {
		hotel int64
		key   string
	}
	var hotels []hk
	rows, err = s.pool.Query(ctx, `SELECT DISTINCT hotel_id, characteristic_key FROM recompute_tasks`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t hk
		if err := rows.Scan(&t.hotel, &t.key); err != nil {
			rows.Close()
			return err
		}
		hotels = append(hotels, t)
	}
	rows.Close()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, q := range []string{
		`DELETE FROM evidence WHERE author_type = 'guest'`,
		`DELETE FROM author_trust WHERE author_type = 'guest'`,
		`DELETE FROM recompute_tasks`,
		`DELETE FROM agent_trace WHERE agent = 'guest_signal'`,
		`DELETE FROM bookings WHERE guest_id IS NOT NULL`,
		`DELETE FROM guests`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, t := range cats {
		id, err := strconv.ParseInt(t.id, 10, 64)
		if err != nil {
			return err
		}
		if err := saveEvidence(ctx, s.pool, s.rules, categoryScope(id), t.key, nil, nil); err != nil {
			return err
		}
	}
	for _, t := range hotels {
		if err := saveEvidence(ctx, s.pool, s.rules, hotelScope(t.hotel), t.key, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *GuestStore) PendingBookings(ctx context.Context, flt domain.HotelFilter) ([]guests.Booking, error) {
	rows, err := s.pool.Query(ctx, `
SELECT b.id, g.id, g.name, g.persona, b.hotel_id, b.room_category_id, b.facade_id, b.floor, b.expectations
FROM bookings b
JOIN guests g ON g.id = b.guest_id
JOIN hotels h ON h.id = b.hotel_id
LEFT JOIN guest_surveys s ON s.booking_id = b.id
WHERE s.id IS NULL AND ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2)
ORDER BY b.id`, flt.RegionID, flt.HotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []guests.Booking
	for rows.Next() {
		var b guests.Booking
		var exp []byte
		if err := rows.Scan(&b.ID, &b.GuestID, &b.GuestName, &b.Persona, &b.HotelID, &b.CategoryID, &b.FacadeID, &b.Floor, &exp); err != nil {
			return nil, err
		}
		var e struct {
			Keys []string `json:"keys"`
		}
		if err := json.Unmarshal(exp, &e); err != nil {
			return nil, err
		}
		b.Expectations = e.Keys
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *GuestStore) SaveSurvey(ctx context.Context, rec guests.SurveyRecord, evs []domain.Evidence) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q, err := json.Marshal(rec.Questions)
	if err != nil {
		return err
	}
	a, err := json.Marshal(rec.Answers)
	if err != nil {
		return err
	}
	care, err := json.Marshal(append([]string{}, rec.Careless...))
	if err != nil {
		return err
	}
	sc, err := json.Marshal(rec.Scope)
	if err != nil {
		return err
	}
	var surveyID int64
	if err := tx.QueryRow(ctx, `
INSERT INTO guest_surveys (guest_id, booking_id, questions, answers, control_ok, duration_sec, careless, scope, points)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		rec.GuestID, rec.BookingID, q, a, rec.ControlOK, rec.DurationSec, care, sc, rec.Points).Scan(&surveyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE guests SET guru_points = guru_points + $2 WHERE id = $1`, rec.GuestID, rec.Points); err != nil {
		return err
	}
	author := domain.Author{Type: domain.AuthorGuest, ID: strconv.FormatInt(rec.GuestID, 10)}
	for _, ev := range evs {
		ev.Details["survey_id"] = surveyID
		id, err := strconv.ParseInt(ev.EntityID, 10, 64)
		if err != nil {
			return err
		}
		if err := saveEvidenceTx(ctx, tx, s.rules, categoryScope(id), ev.Key, []domain.Author{author}, []domain.Evidence{ev}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GuestSurveyCounts — добросовестные анкеты гостя и все.
func (s *GuestStore) GuestSurveyCounts(ctx context.Context, guestID int64) (int, int, error) {
	var reliable, total int
	err := s.pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE control_ok AND careless = '[]'::jsonb), count(*) FROM guest_surveys WHERE guest_id = $1`, guestID).
		Scan(&reliable, &total)
	return reliable, total, err
}

func (s *GuestStore) GuestVotes(ctx context.Context, flt domain.HotelFilter) ([]guests.Vote, error) {
	rows, err := s.pool.Query(ctx, `
SELECT (e.details->>'booking_id')::bigint, e.author_id::bigint, COALESCE(t.trust, $3), c.hotel_id, c.id,
       e.characteristic_key, e.claimed_value, (e.details->>'facade_id')::bigint, (e.details->>'floor')::int
FROM evidence e
JOIN room_categories c ON e.entity_type = $4 AND e.entity_id = c.id::text
JOIN hotels h ON h.id = c.hotel_id
LEFT JOIN author_trust t ON t.author_type = e.author_type AND t.author_id = e.author_id
WHERE e.author_type = $5 AND e.status = $6 AND ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2)
ORDER BY e.id`, flt.RegionID, flt.HotelID, domain.DefaultTrust, domain.EntityRoomCategory, domain.AuthorGuest, domain.FactActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []guests.Vote
	for rows.Next() {
		var v guests.Vote
		var claimed []byte
		if err := rows.Scan(&v.BookingID, &v.GuestID, &v.Trust, &v.HotelID, &v.CategoryID, &v.Key, &claimed, &v.FacadeID, &v.Floor); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(claimed, &m); err != nil {
			return nil, err
		}
		field := s.rules[v.Key].ByEntity[domain.EntityRoomCategory].ValueField
		v.Value, _ = m[field].(string)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *GuestStore) MarkStale(ctx context.Context, hotelID int64, key string, facadeIDs []int64) error {
	ids := make([]string, len(facadeIDs))
	for i, id := range facadeIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	_, err := s.pool.Exec(ctx, `
UPDATE facts SET stale = true
WHERE status = $1 AND characteristic_key = $2 AND entity_type = $3 AND entity_id = ANY($4::text[])
  AND entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $5)`, domain.FactActive, key, domain.EntityFacade, ids, hotelID)
	return err
}

func (s *GuestStore) CreateTask(ctx context.Context, t guests.Task) (bool, error) {
	sig := map[string]any{}
	for k, v := range t.Signal {
		sig[k] = v
	}
	sig["facade_ids"], sig["category_ids"] = t.Facades, t.Categories
	b, err := json.Marshal(sig)
	if err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx, `
INSERT INTO recompute_tasks (hotel_id, characteristic_key, reason, signal) VALUES ($1, $2, $3, $4)
ON CONFLICT (hotel_id, characteristic_key) WHERE status = 'open' DO NOTHING`, t.HotelID, t.Key, t.Reason, b)
	return tag.RowsAffected() > 0, err
}

func (s *GuestStore) OpenTasks(ctx context.Context, flt domain.HotelFilter) ([]guests.Task, error) {
	rows, err := s.pool.Query(ctx, `
SELECT r.id, r.hotel_id, r.characteristic_key, r.reason, r.signal
FROM recompute_tasks r JOIN hotels h ON h.id = r.hotel_id
WHERE r.status = 'open' AND ($1::bigint = 0 OR h.region_id = $1) AND ($2::bigint = 0 OR h.id = $2)
ORDER BY r.id`, flt.RegionID, flt.HotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []guests.Task
	for rows.Next() {
		var t guests.Task
		var sig []byte
		if err := rows.Scan(&t.ID, &t.HotelID, &t.Key, &t.Reason, &sig); err != nil {
			return nil, err
		}
		var m struct {
			Facades    []int64 `json:"facade_ids"`
			Categories []int64 `json:"category_ids"`
		}
		if err := json.Unmarshal(sig, &m); err != nil {
			return nil, err
		}
		t.Facades, t.Categories = m.Facades, m.Categories
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *GuestStore) CompleteTask(ctx context.Context, id int64, outcome string, result map[string]any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE recompute_tasks SET status = 'done', outcome = $2, result = $3, done_at = now() WHERE id = $1`, id, outcome, b)
	return err
}

func (s *GuestStore) FacadeValues(ctx context.Context, hotelID int64, key string) (map[int64]string, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.entity_id::bigint, f.value::text FROM facts f JOIN facades x ON x.id::text = f.entity_id
WHERE f.status = $1 AND f.characteristic_key = $2 AND f.entity_type = $3 AND x.hotel_id = $4`,
		domain.FactActive, key, domain.EntityFacade, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

func (s *GuestStore) CategoryState(ctx context.Context, categoryID int64, key string) (string, float64, string, error) {
	var value []byte
	var conf float64
	err := s.pool.QueryRow(ctx, `
SELECT value, confidence FROM facts WHERE status = $1 AND characteristic_key = $2 AND entity_type = $3 AND entity_id = $4`,
		domain.FactActive, key, domain.EntityRoomCategory, strconv.FormatInt(categoryID, 10)).Scan(&value, &conf)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, "", nil
	}
	if err != nil {
		return "", 0, "", fmt.Errorf("факт категории %d: %w", categoryID, err)
	}
	var m map[string]any
	if err := json.Unmarshal(value, &m); err != nil {
		return "", 0, "", err
	}
	v, _ := m[s.rules[key].ByEntity[domain.EntityRoomCategory].ValueField].(string)
	var conflict string
	err = s.pool.QueryRow(ctx, `
SELECT COALESCE(description, '') FROM conflicts WHERE status = $1 AND characteristic_key = $2 AND entity_type = $3 AND entity_id = $4
ORDER BY id DESC LIMIT 1`, domain.ConflictOpen, key, domain.EntityRoomCategory, strconv.FormatInt(categoryID, 10)).Scan(&conflict)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", 0, "", err
	}
	return v, conf, conflict, nil
}

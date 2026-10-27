package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/metrics"
)

// MetricsStore — чтение данных для collect metrics. Только SELECT: факты, свидетельства и вопросы не меняются.
type MetricsStore struct {
	pool *pgxpool.Pool
}

func NewMetricsStore(pool *pgxpool.Pool) *MetricsStore { return &MetricsStore{pool: pool} }

func (s *MetricsStore) DemoHotels(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM hotels WHERE demo`).Scan(&n)
	return n, err
}

// HotelFacts — активные факты уровня отеля у демо-отелей.
func (s *MetricsStore) HotelFacts(ctx context.Context) ([]metrics.HotelFact, error) {
	rows, err := s.pool.Query(ctx, `
SELECT h.id, f.characteristic_key, f.confidence, f.value
FROM facts f JOIN hotels h ON f.entity_id = h.id::text
WHERE f.status = 'active' AND f.entity_type = 'hotel' AND h.demo
ORDER BY h.id, f.characteristic_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []metrics.HotelFact
	for rows.Next() {
		var f metrics.HotelFact
		var raw []byte
		if err := rows.Scan(&f.HotelID, &f.Key, &f.Confidence, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &f.Value); err != nil {
			return nil, fmt.Errorf("значение факта %s отеля %d: %w", f.Key, f.HotelID, err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Questions — вопросы интервьюера демо-отелям.
func (s *MetricsStore) Questions(ctx context.Context) ([]metrics.QuestionRow, error) {
	rows, err := s.pool.Query(ctx, `
SELECT q.hotel_id, q.characteristic_key, q.status, q.sent_at IS NOT NULL
FROM hotelier_questions q JOIN hotels h ON h.id = q.hotel_id
WHERE h.demo
ORDER BY q.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []metrics.QuestionRow
	for rows.Next() {
		var q metrics.QuestionRow
		if err := rows.Scan(&q.HotelID, &q.Key, &q.Status, &q.Sent); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// FacadeNoise — оценки шума фасадов всех отелей (с признаком демо) по двум источникам (свидетельства bcn_noise и osm).
func (s *MetricsStore) FacadeNoise(ctx context.Context) ([]metrics.FacadeNoise, error) {
	rows, err := s.pool.Query(ctx, `
SELECT fa.id, h.demo,
       COALESCE(bcn.claimed_value->>'level', ''), COALESCE(osm.claimed_value->>'level', ''),
       (bcn.claimed_value->>'ln_db')::float8, (osm.claimed_value->>'ln_db')::float8
FROM facades fa
JOIN hotels h ON h.id = fa.hotel_id
LEFT JOIN evidence bcn ON bcn.entity_type = 'facade' AND bcn.entity_id = fa.id::text AND bcn.characteristic_key = 'noise_night'
     AND bcn.source = 'bcn_noise' AND bcn.status = 'active'
LEFT JOIN evidence osm ON osm.entity_type = 'facade' AND osm.entity_id = fa.id::text AND osm.characteristic_key = 'noise_night'
     AND osm.source = 'osm' AND osm.status = 'active'
ORDER BY fa.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []metrics.FacadeNoise
	for rows.Next() {
		var f metrics.FacadeNoise
		if err := rows.Scan(&f.FacadeID, &f.DemoHotel, &f.BCNLevel, &f.OSMLevel, &f.BCNLn, &f.OSMLn); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

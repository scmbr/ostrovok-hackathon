package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// ClimateStore — запись климатических свидетельств по районам.
type ClimateStore struct {
	pool *pgxpool.Pool
	evidenceWriter
}

func NewClimateStore(pool *pgxpool.Pool, rules map[string]domain.TruthParams) *ClimateStore {
	return &ClimateStore{pool: pool, evidenceWriter: evidenceWriter{pool: pool, rules: rules}}
}

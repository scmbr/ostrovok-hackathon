package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// ClimateStore — запись климатических свидетельств по районам.
type ClimateStore struct {
	pool *pgxpool.Pool
	evidenceWriter
}

func NewClimateStore(pool *pgxpool.Pool) *ClimateStore {
	return &ClimateStore{pool: pool, evidenceWriter: evidenceWriter{pool}}
}

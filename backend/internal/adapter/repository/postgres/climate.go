package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// ClimateStore — запись климатических фактов по районам.
type ClimateStore struct {
	pool *pgxpool.Pool
}

func NewClimateStore(pool *pgxpool.Pool) *ClimateStore { return &ClimateStore{pool: pool} }

// SaveDistrictFacts заменяет активные факты и свидетельства характеристики key у района.
func (s *ClimateStore) SaveDistrictFacts(ctx context.Context, district, key string, facts []domain.Fact) error {
	return saveDistrictFacts(ctx, s.pool, district, key, facts)
}

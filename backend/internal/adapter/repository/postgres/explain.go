package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// ExplainStore читает свидетельства для «под капотом» (usecase/explain).
type ExplainStore struct{ pool *pgxpool.Pool }

func NewExplainStore(pool *pgxpool.Pool) *ExplainStore { return &ExplainStore{pool: pool} }

func (s *ExplainStore) HotelEvidence(ctx context.Context, hotelID int64) ([]domain.Evidence, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+evidenceCols+` FROM evidence
WHERE status = $2 AND characteristic_key <> 'room_placement' AND (
      (entity_type = 'hotel' AND entity_id = ($1::bigint)::text)
   OR (entity_type = 'facade' AND entity_id IN (SELECT id::text FROM facades WHERE hotel_id = $1::bigint))
   OR (entity_type = 'room_category' AND entity_id IN (SELECT id::text FROM room_categories WHERE hotel_id = $1::bigint)))
ORDER BY entity_type, entity_id, characteristic_key, id`, hotelID, domain.FactActive)
	if err != nil {
		return nil, err
	}
	return scanEvidence(rows)
}

func (s *ExplainStore) EntityEvidence(ctx context.Context, entityType, entityID, key string) ([]domain.Evidence, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+evidenceCols+` FROM evidence
WHERE status = $4 AND entity_type = $1 AND entity_id = $2 AND characteristic_key = $3 ORDER BY id`,
		entityType, entityID, key, domain.FactActive)
	if err != nil {
		return nil, err
	}
	return scanEvidence(rows)
}

func (s *ExplainStore) Trusts(ctx context.Context) (map[domain.Author]float64, error) {
	return loadTrust(ctx, s.pool)
}

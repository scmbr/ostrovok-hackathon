package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

// CustomStore хранит характеристики, добавленные текстом (custom_characteristics).
type CustomStore struct {
	pool     *pgxpool.Pool
	regionID int64
}

func NewCustomStore(pool *pgxpool.Pool, regionID int64) *CustomStore {
	return &CustomStore{pool: pool, regionID: regionID}
}

// ListCustom возвращает добавленные характеристики региона в порядке добавления.
func (s *CustomStore) ListCustom(ctx context.Context) ([]registry.CustomRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, yaml, origin_text, added_at FROM custom_characteristics WHERE region_id = $1 ORDER BY created_at, key`, s.regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []registry.CustomRow
	for rows.Next() {
		var r registry.CustomRow
		var at time.Time
		if err := rows.Scan(&r.Key, &r.YAML, &r.OriginText, &at); err != nil {
			return nil, err
		}
		r.AddedAt = at.Format(time.DateOnly)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveCustom записывает характеристику; ключ уже в таблице — ошибка (тихой перезаписи нет).
func (s *CustomStore) SaveCustom(ctx context.Context, r registry.CustomRow) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO custom_characteristics (key, region_id, yaml, origin_text, added_at) VALUES ($1, $2, $3, $4, $5::date)`,
		r.Key, s.regionID, r.YAML, r.OriginText, r.AddedAt)
	return err
}

// DeleteCustom убирает характеристику (откат неудачного сохранения).
func (s *CustomStore) DeleteCustom(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM custom_characteristics WHERE key = $1 AND region_id = $2`, key, s.regionID)
	return err
}

// Package postgres — реализация портов хранения на Postgres + PostGIS.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect открывает пул и ждёт доступности БД (compose уже ждёт healthcheck, это страховка).
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i := 0; i < 10; i++ {
		if lastErr = pool.Ping(ctx); lastErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	pool.Close()
	return nil, fmt.Errorf("БД недоступна: %w", lastErr)
}

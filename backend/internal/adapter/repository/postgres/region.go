package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// RegionStore читает регионы: контур охвата и районы.
type RegionStore struct {
	pool *pgxpool.Pool
}

func NewRegionStore(pool *pgxpool.Pool) *RegionStore { return &RegionStore{pool: pool} }

// LoadRegion возвращает регион по коду с районами в порядке приоритета.
func (s *RegionStore) LoadRegion(ctx context.Context, code string) (domain.Region, error) {
	var r domain.Region
	err := s.pool.QueryRow(ctx, `
SELECT id, code, name, ST_YMin(contour), ST_XMin(contour), ST_YMax(contour), ST_XMax(contour)
FROM regions WHERE code = $1 AND contour IS NOT NULL`, code).
		Scan(&r.ID, &r.Code, &r.Name, &r.Contour.South, &r.Contour.West, &r.Contour.North, &r.Contour.East)
	if err == pgx.ErrNoRows {
		return r, fmt.Errorf("регион %q не найден или без контура", code)
	}
	if err != nil {
		return r, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT name, south, west, north, east FROM districts WHERE region_id = $1 ORDER BY priority`, r.ID)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var d domain.District
		if err := rows.Scan(&d.Name, &d.BBox.South, &d.BBox.West, &d.BBox.North, &d.BBox.East); err != nil {
			return r, err
		}
		r.Districts = append(r.Districts, d)
	}
	return r, rows.Err()
}

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/lookup"
)

const lookupSelect = `SELECT h.id, h.name, COALESCE(h.district, ''), h.card FROM hotels h`

func scanLookup(row pgx.Row) (lookup.Hotel, error) {
	var h lookup.Hotel
	var card []byte
	if err := row.Scan(&h.ID, &h.Name, &h.District, &card); err != nil {
		return h, err
	}
	if err := json.Unmarshal(card, &h.Card); err != nil {
		return h, fmt.Errorf("карточка отеля %d: %w", h.ID, err)
	}
	return h, nil
}

// HotelByID возвращает демо-отель региона по id.
func (s *SearchStore) HotelByID(ctx context.Context, regionID, id int64) (lookup.Hotel, bool, error) {
	h, err := scanLookup(s.pool.QueryRow(ctx, lookupSelect+` WHERE h.region_id = $1 AND h.demo AND h.id = $2`, regionID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return lookup.Hotel{}, false, nil
	}
	return h, err == nil, err
}

// HotelsLike отбирает демо-отели, у которых название или район содержат хотя бы одно слово (ILIKE без диакритики).
// Расширение pg_trgm не используется: оно потребовало бы миграции.
func (s *SearchStore) HotelsLike(ctx context.Context, regionID int64, tokens []string) ([]lookup.Hotel, error) {
	patterns := make([]string, len(tokens))
	for i, t := range tokens {
		patterns[i] = "%" + t + "%"
	}
	rows, err := s.pool.Query(ctx, lookupSelect+`
WHERE h.region_id = $1 AND h.demo
  AND translate(lower(h.name || ' ' || COALESCE(h.district, '')), 'áàâäéèêëíìîïóòôöúùûüñç', 'aaaaeeeeiiiioooouuuunc') ILIKE ANY ($2)
ORDER BY h.id`, regionID, patterns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lookup.Hotel
	for rows.Next() {
		h, err := scanLookup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

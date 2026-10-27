package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
)

// BookingStore — брони из диалога (заявки без оплаты). Имитированные брони гостей «Гуру» лежат в той же таблице, но с guest_id.
type BookingStore struct{ pool *pgxpool.Pool }

func NewBookingStore(pool *pgxpool.Pool) *BookingStore { return &BookingStore{pool: pool} }

func (s *BookingStore) CreateBooking(ctx context.Context, r booking.Record) (booking.Record, error) {
	wishes := map[string]any{}
	if r.Wish != nil {
		wishes = r.Wish
	}
	wb, err := json.Marshal(wishes)
	if err != nil {
		return r, err
	}
	eb, err := json.Marshal(r.Expectations)
	if err != nil {
		return r, err
	}
	var qid *int64
	if r.WishQuestionID != 0 {
		qid = &r.WishQuestionID
	}
	err = s.pool.QueryRow(ctx, `
INSERT INTO bookings (hotel_id, room_category_id, wishes, expectations, session_id, wish_question_id)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		r.HotelID, r.CategoryID, wb, eb, r.SessionID, qid).Scan(&r.ID, &r.CreatedAt)
	return r, err
}

func (s *BookingStore) Booking(ctx context.Context, id int64) (booking.Record, error) {
	var r booking.Record
	var wb, eb []byte
	var cat, qid *int64
	var session *string
	err := s.pool.QueryRow(ctx, `
SELECT id, hotel_id, room_category_id, wishes, expectations, session_id, wish_question_id, created_at
FROM bookings WHERE id = $1`, id).Scan(&r.ID, &r.HotelID, &cat, &wb, &eb, &session, &qid, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, booking.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if cat != nil {
		r.CategoryID = *cat
	}
	if qid != nil {
		r.WishQuestionID = *qid
	}
	if session != nil {
		r.SessionID = *session
	}
	var wish map[string]any
	if err := json.Unmarshal(wb, &wish); err == nil && len(wish) > 0 {
		r.Wish = wish
	}
	var lines []domain.ContractLine
	_ = json.Unmarshal(eb, &lines) // имитированные брони хранят в expectations другой формат
	r.Expectations = lines
	return r, nil
}

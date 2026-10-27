package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DialogStore — хранилище диалога: состояние сессий и журнал неудовлетворённых желаний.
// Сессии живут в Postgres (docs/dialog.md): несколько экземпляров сервера и перезапуск не теряют разговор.
type DialogStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewDialogStore(pool *pgxpool.Pool, ttl time.Duration) *DialogStore {
	return &DialogStore{pool: pool, ttl: ttl}
}

// LoadState читает состояние сессии. Строки старше TTL удаляются при каждом чтении, отдельного фонового процесса нет.
func (s *DialogStore) LoadState(ctx context.Context, sessionID string) ([]byte, bool, error) {
	if _, err := s.pool.Exec(ctx, `DELETE FROM dialog_sessions WHERE updated_at < now() - make_interval(secs => $1)`, s.ttl.Seconds()); err != nil {
		return nil, false, err
	}
	var state []byte
	err := s.pool.QueryRow(ctx, `SELECT state FROM dialog_sessions WHERE session_id = $1`, sessionID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return state, err == nil, err
}

// SaveState записывает состояние и продлевает сессию.
func (s *DialogStore) SaveState(ctx context.Context, sessionID string, state []byte) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO dialog_sessions (session_id, state, updated_at) VALUES ($1, $2, now())
ON CONFLICT (session_id) DO UPDATE SET state = EXCLUDED.state, updated_at = now()`, sessionID, state)
	return err
}

// LogUnmet записывает желание, которого нет в реестре; повтор той же нормализованной формы увеличивает count.
// proposal (может быть nil) записывается, только если у строки его ещё нет. Возвращает счётчик после записи.
func (s *DialogStore) LogUnmet(ctx context.Context, regionID int64, text, normalized string, proposal []byte) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
INSERT INTO unmet_wishes (region_id, text, normalized, proposal) VALUES ($1, $2, $3, $4)
ON CONFLICT (region_id, normalized) DO UPDATE
   SET count = unmet_wishes.count + 1, last_seen = now(), proposal = COALESCE(unmet_wishes.proposal, EXCLUDED.proposal)
RETURNING count`, regionID, text, normalized, proposal).Scan(&count)
	return count, err
}

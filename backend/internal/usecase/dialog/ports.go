package dialog

import (
	"context"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// Searcher — поиск 6.1 (search.Service).
type Searcher interface {
	Search(ctx context.Context, q domain.Query) (search.Result, error)
	Similar(ctx context.Context, id int64, limit int, weights map[string]float64) ([]search.SimilarHotel, error)
	Hotels(ctx context.Context) ([]search.Candidate, error)
}

// SessionStore — состояние сессий в Postgres; устаревшие строки хранилище удаляет при чтении.
type SessionStore interface {
	LoadState(ctx context.Context, sessionID string) (state []byte, found bool, err error)
	SaveState(ctx context.Context, sessionID string, state []byte) error
}

// UnmetStore — журнал желаний, которых нет в реестре. Возвращает, сколько раз желание уже встречалось.
type UnmetStore interface {
	LogUnmet(ctx context.Context, regionID int64, text, normalized string, proposal []byte) (count int, err error)
}

// TraceWriter — журнал решений агента (agent_trace).
type TraceWriter interface {
	AppendTrace(ctx context.Context, s agent.TraceStep) error
}

// Rephraser — необязательный пересказ шаблонного текста моделью. Текст проходит проверку кодом (VerifyRephrase).
type Rephraser interface {
	Rephrase(ctx context.Context, template string) (string, error)
}

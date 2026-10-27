package booking

import (
	"context"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// Searcher — оценка отелей по запросу гостя (search.Service).
type Searcher interface {
	EvalHotel(ctx context.Context, hotelID int64, q domain.Query) (search.HotelVariants, domain.Query, error)
	Hotels(ctx context.Context) ([]search.Candidate, error)
}

// SessionStore — сессии диалога в Postgres. Состояние читается и пишется как JSON: неизвестные этому пакету поля
// (память диалога) сохраняются как есть.
type SessionStore interface {
	LoadState(ctx context.Context, sessionID string) (state []byte, found bool, err error)
	SaveState(ctx context.Context, sessionID string, state []byte) error
}

// Hotelier — вопросы отельеру по инициативе гостя (hotelier.Interviewer).
type Hotelier interface {
	AskAbout(ctx context.Context, hotelID int64, key string) (hotelier.Ask, error)
	ConfirmWish(ctx context.Context, hotelID int64, category, wish string) (hotelier.Ask, error)
	WishAnswer(ctx context.Context, hotelID int64) (answered, guarantees bool, err error)
}

// DemandRecorder — запись спроса: одна сессия увеличивает спрос по ключу один раз.
type DemandRecorder interface {
	Record(ctx context.Context, regionID int64, sessionID string, keys []string) error
}

// TraceWriter — журнал решений агента (agent_trace).
type TraceWriter interface {
	AppendTrace(ctx context.Context, s agent.TraceStep) error
}

// Record — бронь в хранилище.
type Record struct {
	ID         int64
	SessionID  string
	HotelID    int64
	CategoryID int64
	// Wish — пожелание к номеру (фасад, этаж, текст) или nil; Expectations — снимок контракта ожиданий.
	Wish           map[string]any
	Expectations   []domain.ContractLine
	WishQuestionID int64
	CreatedAt      time.Time
}

// Store — хранилище броней.
type Store interface {
	CreateBooking(ctx context.Context, r Record) (Record, error)
	Booking(ctx context.Context, id int64) (Record, error)
}

// Пакет guests — гости «Гуру»: имитированные заселения, анкеты после проживания, репутация гостя, сигнал пересчёта.
// Ответ гостя — свидетельство о категории номеров, привязанное к фасаду и этажу брони; факт собирает движок правды.
// Небрежность и контрольный вопрос снижают репутацию, несогласие с расчётом — нет. Описание — docs/guests.md.
package guests

import (
	"context"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/reputation"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// Имена в agent_trace.
const (
	AgentSignal = "guest_signal"
	// SurveyMethod — метод свидетельства гостя.
	SurveyMethod = "guest_survey@1"
)

// Hotel — демо-отель для мока заселений.
type Hotel struct {
	ID   int64
	Name string
	// Embellishing — у отеля открытый конфликт названия категории («Sea View») с расчётом. Заселений у таких отелей
	// не делаем: их сценарий — репутация отельера (docs/hotelier.md), гости его не меняют.
	Embellishing bool
}

// Stay — имитированное заселение: гость с персоной и бронь с фасадом и этажом.
type Stay struct {
	GuestName    string
	Persona      string
	HotelID      int64
	CategoryID   int64
	FacadeID     int64
	Floor        int
	Wishes       map[string]any
	Expectations []string
}

// Booking — бронь, по которой нужна анкета.
type Booking struct {
	ID           int64
	GuestID      int64
	GuestName    string
	Persona      string
	HotelID      int64
	CategoryID   int64
	FacadeID     int64
	Floor        int
	Expectations []string
}

// SurveyRecord — анкета для записи.
type SurveyRecord struct {
	GuestID     int64
	BookingID   int64
	Questions   []map[string]any
	Answers     []map[string]any
	ControlOK   bool
	DurationSec int
	Careless    []string
	Scope       []map[string]any
	Points      int
}

// Vote — ответ гостя, вошедший в свидетельства о категории: исходные данные сигнала.
type Vote struct {
	BookingID, GuestID int64
	Trust              float64
	HotelID            int64
	CategoryID         int64
	Key                string
	Value              string
	FacadeID           int64
	Floor              int
}

// Task — задача пересчёта: характеристика отеля, поставленная на пересчёт сигналом гостей.
type Task struct {
	ID         int64
	HotelID    int64
	Key        string
	Reason     string
	Facades    []int64 // фасады, у которых факт помечен stale
	Categories []int64 // категории, чьи гости дали сигнал
	Signal     map[string]any
}

// Store — порт хранилища гостей. Факт через него записать нельзя: только свидетельства.
type Store interface {
	DemoHotels(ctx context.Context, flt domain.HotelFilter) ([]Hotel, error)
	// ResetGuests удаляет гостей, брони, анкеты, их свидетельства и репутацию, задачи пересчёта и журнал сигнала;
	// факты затронутых категорий и фасадов пересобираются.
	ResetGuests(ctx context.Context) error
	SaveStays(ctx context.Context, stays []Stay) error
	// PendingBookings — брони гостей без анкеты.
	PendingBookings(ctx context.Context, flt domain.HotelFilter) ([]Booking, error)
	HotelFacts(ctx context.Context, hotelID int64) ([]domain.Fact, error)
	// SaveSurvey пишет анкету, баллы гостя и свидетельства evs о категориях (прежнее свидетельство гостя по той же
	// категории и характеристике снимается), факты категорий пересобирает движок правды.
	SaveSurvey(ctx context.Context, rec SurveyRecord, evs []domain.Evidence) error
	// GuestVotes — активные ответы гостей о категориях с репутацией авторов.
	GuestVotes(ctx context.Context, flt domain.HotelFilter) ([]Vote, error)
	// MarkStale ставит активные факты характеристики у фасадов отеля на пересчёт (stale).
	MarkStale(ctx context.Context, hotelID int64, key string, facadeIDs []int64) error
	// CreateTask пишет задачу пересчёта; created=false, если открытая такая уже есть.
	CreateTask(ctx context.Context, t Task) (created bool, err error)
	OpenTasks(ctx context.Context, flt domain.HotelFilter) ([]Task, error)
	// CompleteTask закрывает задачу с исходом confirmed | changed.
	CompleteTask(ctx context.Context, id int64, outcome string, result map[string]any) error
	// FacadeValues — значения характеристики у фасадов отеля по активным фактам (значение целиком, в виде JSON).
	FacadeValues(ctx context.Context, hotelID int64, key string) (map[int64]string, error)
	// CategoryState — итог движка правды по категории: значение, уверенность и описание открытого конфликта.
	CategoryState(ctx context.Context, categoryID int64, key string) (value string, confidence float64, conflict string, err error)
	Hotel(ctx context.Context, id int64) (agent.HotelInfo, error)
	AppendTrace(ctx context.Context, s agent.TraceStep) error
}

// Rooms — ячейки «фасад × этаж» и категории отеля (rooms.Service).
type Rooms interface {
	Cells(ctx context.Context, hotelID int64) (*rooms.Cells, error)
}

// Reputation — пересчёт репутации гостя.
type Reputation interface {
	UpdateGuest(ctx context.Context, guestID int64) (reputation.Update, error)
}

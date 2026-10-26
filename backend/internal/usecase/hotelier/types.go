// Package hotelier — агент-интервьюер отельера: выбирает вопросы по ценности и бюджету, формулирует их,
// разбирает свободные ответы по полям характеристики и пишет свидетельства отельера. Значение факта он записать
// не может: факт собирает движок правды из свидетельств.
package hotelier

import (
	"context"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// AgentName — имя агента в agent_trace.
const AgentName = "hotelier_interviewer"

// Статусы вопроса (hotelier_questions.status).
const (
	StatusPending            = "pending"           // прошёл порог, ждёт бюджета
	StatusLowValue           = "pending_low_value" // ценность ниже порога, копится
	StatusOpen               = "open"              // отправлен, ждёт ответа
	StatusNeedsClarification = "needs_clarification"
	StatusAnswered           = "answered"
	StatusClosed             = "closed"
)

// Hotel — отель, с которым говорит интервьюер.
type Hotel struct {
	ID       int64
	Name     string
	District string
	RegionID int64
	Persona  string // персона имитированного отельера; пусто — отельер живой
}

// Score — из чего сложена ценность вопроса (hotelier_questions.score).
type Score struct {
	Demand      int     `json:"demand"`      // гостей искали характеристику
	DemandNorm  float64 `json:"demand_norm"` // доля от максимального спроса в регионе
	Uncertainty float64 `json:"uncertainty"`
	Impact      float64 `json:"impact"`
	Value       float64 `json:"value"`
	// CategoriesChanged и Categories — у скольких категорий ответ меняет вероятность (характеристики фасада).
	CategoriesChanged int `json:"categories_changed"`
	Categories        int `json:"categories"`
}

// Question — вопрос отельеру.
type Question struct {
	ID         int64
	HotelID    int64
	Key        string
	Text       string
	Reason     string
	Status     string
	Format     string
	Persona    string
	Score      Score
	AnswerText string
	Parsed     map[string]any
	SentAt     *time.Time
	AskedAt    time.Time
}

// Store — порт хранилища интервьюера. Записать значение факта через него нельзя.
type Store interface {
	Hotel(ctx context.Context, id int64) (Hotel, error)
	HotelFacts(ctx context.Context, hotelID int64) ([]domain.Fact, error)
	Demand(ctx context.Context, regionID int64) (map[string]int, error)
	// Trust — репутация отельера (author_trust); нет записи — domain.DefaultTrust.
	Trust(ctx context.Context, a domain.Author) (float64, error)
	// Truth — скрытая истина мока по характеристике (только для имитированных отельеров).
	Truth(ctx context.Context, hotelID int64, key string) (map[string]any, bool, error)
	// Questions — вопросы отеля со всеми статусами.
	Questions(ctx context.Context, hotelID int64) ([]Question, error)
	// SentSince — сколько вопросов отправлено отельцу с момента since.
	SentSince(ctx context.Context, hotelID int64, since time.Time) (int, error)
	// UpsertCandidate пишет кандидата (pending или pending_low_value): новый или обновляет копящийся вопрос.
	UpsertCandidate(ctx context.Context, q Question) (Question, error)
	// Send переводит копящийся вопрос в open с текстом и форматом.
	Send(ctx context.Context, id int64, text, format, persona string, at time.Time) error
	// RecordAnswer сохраняет текст ответа, разбор и новый статус вопроса.
	RecordAnswer(ctx context.Context, id int64, status, answerText string, parsed map[string]any, at time.Time) error
	// Close закрывает копящийся вопрос: данные появились сами.
	Close(ctx context.Context, id int64, note string) error
	// SaveAnswerEvidence записывает свидетельство отельера; прежнее активное свидетельство того же автора
	// по той же сущности и характеристике снимается (superseded), факт пересобирает движок правды.
	SaveAnswerEvidence(ctx context.Context, ev domain.Evidence) error
	AppendTrace(ctx context.Context, s agent.TraceStep) error
}

// Impact — влияние ответа на категории номеров (rooms.Service).
type Impact interface {
	CategoryImpact(ctx context.Context, hotelID int64, key string) (changed, total int, err error)
}

// LLM — порт модели для формулировки вопроса и разбора ответа. Любая ошибка (нет ключа, промах кеша, сеть)
// означает детерминированный запасной вариант.
type LLM interface {
	Complete(ctx context.Context, system, user string) (string, error)
	// Mode — llm (живая модель) или cache (сохранённые ответы).
	Mode() string
}

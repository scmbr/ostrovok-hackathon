package agent

import (
	"context"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Режимы работы агента (agent_trace.mode).
const (
	ModeLLM     = "llm"
	ModeCache   = "cache"
	ModePlanner = "planner"
)

// AgentName — имя агента-сборщика в agent_trace.
const AgentName = "collector"

// Пороги правил агента (до бюджета вопросов этапа 5).
const (
	// FreshConfidence — факт с такой уверенностью и выше, не stale и без weak_disagreement, агент пропускает.
	FreshConfidence = 0.75
	// AskConfidence — вопрос отельеру создаётся, если уверенность ниже или данных нет.
	AskConfidence = 0.6
	// MaxQuestions — вопросов отельеру на отель за один прогон.
	MaxQuestions = 3
	// MaxSteps — шагов агента (решений) за прогон.
	MaxSteps = 20
)

// HotelInfo — отель, по которому идёт прогон.
type HotelInfo struct {
	ID       int64
	Name     string
	District string
	Demo     bool
}

// TraceStep — одна запись журнала.
type TraceStep struct {
	ID        int64          `json:"id,omitempty"`
	Agent     string         `json:"agent"`
	SessionID string         `json:"session_id"`
	HotelID   int64          `json:"hotel_id"`
	Mode      string         `json:"mode"`
	Step      int            `json:"step"`
	Decision  string         `json:"decision"`
	Tool      string         `json:"tool,omitempty"`
	Input     map[string]any `json:"input"`
	Output    map[string]any `json:"output"`
	CreatedAt time.Time      `json:"created_at"`
}

// Question — вопрос отельеру (запись в hotelier_questions).
type Question struct {
	HotelID    int64
	Key        string
	Text       string
	Reason     string
	ValueScore float64
}

// Store — порт хранилища агента. Записать значение факта через него нельзя: факты собирает только движок правды.
type Store interface {
	Hotel(ctx context.Context, id int64) (HotelInfo, error)
	// HotelFacts — активные факты отеля, его фасадов и района.
	HotelFacts(ctx context.Context, hotelID int64) ([]domain.Fact, error)
	// RecomputeFact пересобирает факты характеристики движком правды из активных свидетельств.
	RecomputeFact(ctx context.Context, hotelID int64, district, key string) error
	// CreateQuestion пишет вопрос; created=false, если такой открытый вопрос уже есть.
	CreateQuestion(ctx context.Context, q Question) (created bool, err error)
	AppendTrace(ctx context.Context, s TraceStep) error
	ListTrace(ctx context.Context, hotelID int64, sessionID string) ([]TraceStep, error)
}

// ToolRunner — порт запуска инструментов расчёта по одному отелю. Инструмент пишет только свидетельства.
type ToolRunner interface {
	// Run запускает инструмент tool по отелю с переопределением числовых параметров; summary — краткий итог.
	Run(ctx context.Context, h HotelInfo, tool string, params map[string]float64) (summary string, err error)
}

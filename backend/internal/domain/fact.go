package domain

import "time"

// Статусы факта (facts.status).
const (
	FactActive     = "active"
	FactSuperseded = "superseded"
	FactDisputed   = "disputed"
)

// Типы сущностей факта (facts.entity_type).
const (
	EntityHotel  = "hotel"
	EntityFacade = "facade"
)

// Источники факта (facts.source).
const SourceComputed = "computed"

// Fact — значение характеристики с происхождением. LLM источником факта быть не может.
type Fact struct {
	ID         int64
	EntityType string
	EntityID   string
	Key        string // characteristic_key
	Value      map[string]any
	Source     string
	Method     string // инструмент и версия алгоритма
	Confidence float64
	ObservedAt time.Time
	ValidUntil *time.Time
	Evidence   map[string]any
}

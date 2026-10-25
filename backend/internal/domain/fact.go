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
const (
	SourceComputed = "computed"
	SourceBCNNoise = "bcn_noise"
)

// Fact — итоговое значение характеристики со ссылками на свидетельства. LLM источником факта быть не может.
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
	Evidence   []Evidence // свидетельства, из которых сложен факт
	// Conflict — расхождение свидетельств факта, найденное при расчёте; сохраняется вместе с фактом.
	Conflict *Conflict
}

// Статусы конфликта (conflicts.status).
const (
	ConflictOpen       = "open"
	ConflictSuperseded = "superseded"
)

// Conflict — расхождение свидетельств по одной сущности и характеристике. Не усредняется: ждёт разрешения
// по приоритету источников или вопроса отельеру (этап 4).
type Conflict struct {
	Description string
}

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
	// EvidenceIDs — свидетельства, из которых сложен факт.
	EvidenceIDs []int64
	// Conflict — два весомых голоса друг против друга, найденные при расчёте; сохраняется вместе с фактом.
	Conflict *Conflict
	// WeakDisagreement — «против» есть, но весит меньше порога конфликта: уверенность снижена, конфликта нет.
	// На этапе 5 поднимает ценность вопроса отельеру.
	WeakDisagreement bool
	// Stale — свидетельство, давшее значение, старше срока годности: характеристика ждёт пересчёта.
	Stale bool
	// Contributions — вклад каждого свидетельства в факт, в порядке EvidenceIDs; пишется в evidence.contribution.
	Contributions []Contribution
}

// Статусы конфликта (conflicts.status).
const (
	ConflictOpen       = "open"
	ConflictSuperseded = "superseded"
)

// Conflict — сильные свидетельства с обеих сторон по одной сущности и характеристике. Не усредняется:
// ждёт вопроса тому, чей ответ дешевле и информативнее.
type Conflict struct {
	Description string
}

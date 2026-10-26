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

// conflictRules — правила обнаружения конфликта по характеристикам. Характеристика без правила конфликтов не даёт.
// Правило получает все активные свидетельства одной сущности и характеристики.
var conflictRules = map[string]func([]Evidence) *Conflict{
	NoiseNightKey: noiseConflict,
}

// AssembleFact собирает факт сущности из её активных свидетельств по правилу «сильнейшее свидетельство»:
// значение, источник, метод, срок и уверенность берутся у свидетельства с наибольшей надёжностью
// (при равенстве — у более раннего). Байесовское сложение заменит правило в задаче 4.2.
// Без свидетельств факта нет (false).
func AssembleFact(es []Evidence) (Fact, bool) {
	if len(es) == 0 {
		return Fact{}, false
	}
	best := es[0]
	ids := make([]int64, 0, len(es))
	for _, e := range es {
		ids = append(ids, e.ID)
		if e.Reliability > best.Reliability {
			best = e
		}
	}
	f := Fact{
		EntityType: best.EntityType, EntityID: best.EntityID, Key: best.Key, Value: best.Claimed,
		Source: best.Source, Method: best.Method, Confidence: best.Reliability,
		ObservedAt: best.ObservedAt, ValidUntil: best.ValidUntil, EvidenceIDs: ids,
	}
	if rule := conflictRules[best.Key]; rule != nil {
		f.Conflict = rule(es)
	}
	return f, true
}

// AssembleFacts собирает по факту на каждую пару (сущность, характеристика) среди свидетельств.
// Порядок фактов — по первому появлению пары, порядок свидетельств внутри факта сохраняется.
func AssembleFacts(es []Evidence) []Fact {
	type pair struct{ entityType, entityID, key string }
	groups := map[pair][]Evidence{}
	var order []pair
	for _, e := range es {
		p := pair{e.EntityType, e.EntityID, e.Key}
		if _, ok := groups[p]; !ok {
			order = append(order, p)
		}
		groups[p] = append(groups[p], e)
	}
	var out []Fact
	for _, p := range order {
		if f, ok := AssembleFact(groups[p]); ok {
			out = append(out, f)
		}
	}
	return out
}

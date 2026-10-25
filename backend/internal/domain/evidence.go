package domain

import (
	"math"
	"time"
)

// Типы авторов свидетельства (evidence.author_type).
const (
	AuthorTool     = "tool"
	AuthorHotelier = "hotelier"
	AuthorGuest    = "guest"
)

// Границы надёжности свидетельства: ни одно не бывает абсолютно верным или совсем бесполезным.
const (
	MinReliability = 0.05
	MaxReliability = 0.97
)

// Evidence — свидетельство: утверждение источника или автора о характеристике сущности.
// Факт собирается из свидетельств; LLM автором свидетельства быть не может.
type Evidence struct {
	ID          int64
	EntityType  string
	EntityID    string
	Key         string // characteristic_key
	Claimed     map[string]any
	Source      string
	Method      string
	AuthorType  string
	AuthorID    string
	Reliability float64 // r в (0,1)
	ObservedAt  time.Time
	ValidUntil  *time.Time
	Details     map[string]any
}

// Contribution — вклад свидетельства в логарифм шансов своего значения: ln(r / (1 − r)).
func (e Evidence) Contribution() float64 {
	r := math.Min(math.Max(e.Reliability, MinReliability), MaxReliability)
	return math.Log(r / (1 - r))
}

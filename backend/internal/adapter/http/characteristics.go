package http

import (
	"net/http"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// characteristicsDTO — ответ GET /api/characteristics: реестр характеристик как его видит клиент.
type characteristicsDTO struct {
	Characteristics []characteristicDTO `json:"characteristics"`
}

type characteristicDTO struct {
	Key             string        `json:"key"`
	Title           string        `json:"title"`
	Description     string        `json:"description"`
	Level           string        `json:"level"`
	ValueType       string        `json:"value_type"`
	Values          []string      `json:"values,omitempty"`
	Ordered         bool          `json:"ordered,omitempty"`
	ValueField      string        `json:"value_field"`
	TTLDays         int           `json:"ttl_days"`
	FavorableValues []string      `json:"favorable_values,omitempty"`
	SignificantDiff float64       `json:"significant_diff,omitempty"`
	Conflict        conflictDTO   `json:"conflict"`
	Strategies      []strategyDTO `json:"strategies"`
	AskHotelier     string        `json:"ask_hotelier,omitempty"`
	AskGuest        string        `json:"ask_guest,omitempty"`
}

// conflictDTO — действующие правила конфликта (с умолчаниями, если реестр их не задаёт).
type conflictDTO struct {
	Steps    int      `json:"steps"`
	Theta    float64  `json:"theta"`
	Entities []string `json:"entities,omitempty"`
}

type strategyDTO struct {
	Tool              string         `json:"tool"`
	Strategy          string         `json:"strategy,omitempty"`
	Params            map[string]any `json:"params,omitempty"`
	BaseReliability   float64        `json:"base_reliability"`
	IndependenceGroup string         `json:"independence_group"`
}

func newCharacteristicsDTO(reg domain.Registry) characteristicsDTO {
	out := characteristicsDTO{Characteristics: []characteristicDTO{}}
	for _, c := range reg.All() {
		rules := c.TruthParams()
		d := characteristicDTO{
			Key: c.Key, Title: c.Title, Description: c.Description, Level: c.Level, ValueType: c.ValueType,
			Values: c.Values, Ordered: c.Ordered, ValueField: c.FieldName(), TTLDays: c.TTLDays,
			FavorableValues: c.FavorableValues, SignificantDiff: c.SignificantDiff,
			Conflict:   conflictDTO{Steps: max(rules.ConflictSteps, 1), Theta: rules.ConflictMinSide, Entities: c.ConflictEntities},
			Strategies: []strategyDTO{}, AskHotelier: c.AskHotelier, AskGuest: c.AskGuest,
		}
		if d.Conflict.Theta <= 0 {
			d.Conflict.Theta = domain.DefaultConflictMinSide
		}
		for _, s := range c.Strategies {
			sd := strategyDTO{Tool: s.Tool, Strategy: s.Variant, BaseReliability: s.BaseReliability, IndependenceGroup: s.IndependenceGroup}
			if len(s.Params)+len(s.Lists) > 0 {
				sd.Params = make(map[string]any, len(s.Params)+len(s.Lists))
				for k, v := range s.Params {
					sd.Params[k] = v
				}
				for k, v := range s.Lists {
					sd.Params[k] = v
				}
			}
			d.Strategies = append(d.Strategies, sd)
		}
		out.Characteristics = append(out.Characteristics, d)
	}
	return out
}

func handleCharacteristics(reg domain.Registry) http.HandlerFunc {
	body := newCharacteristicsDTO(reg) // реестр неизменен до перезапуска
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, body)
	}
}

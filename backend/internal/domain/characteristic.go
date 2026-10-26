package domain

import (
	"fmt"
	"slices"
	"time"
)

// Уровни характеристики: к какой сущности относится значение.
const (
	LevelHotel    = "hotel"
	LevelFacade   = "facade"
	LevelDistrict = "district"
)

// Типы значения характеристики (реестр, раздел 7 PROJECT_CONTEXT.md).
const (
	ValueBool   = "bool"
	ValueEnum   = "enum"
	ValueNumber = "number"
	ValueRange  = "range" // диапазон или структура (этажи, помесячная таблица): значение не сравнивается
	ValueText   = "text"
)

// DefaultValueField — поле значения в claimed, если реестр не называет другое.
const DefaultValueField = "value"

// Strategy — способ получить характеристику: инструмент, его параметры и база надёжности.
type Strategy struct {
	Tool string
	// Variant — вариант инструмента, если он умеет несколько источников (noise-night: bcn, osm).
	Variant string
	// Params и Lists — параметры запуска инструмента: числа и наборы значений (kinds: [pharmacy]).
	Params map[string]float64
	Lists  map[string][]string
	// BaseReliability — база надёжности свидетельства стратегии; у расчётов с переменным качеством данных
	// (sea_view) это потолок, понижение остаётся в расчёте.
	BaseReliability   float64
	IndependenceGroup string
}

// Characteristic — описание характеристики из реестра: что это, как получить, как сверять и когда устареет.
type Characteristic struct {
	Key         string
	Title       string
	Description string
	Level       string
	ValueType   string
	Values      []string // для enum
	// Ordered — значения enum идут по порядку (ступени): расстояние между ними считается в шагах.
	Ordered    bool
	ValueField string // поле значения в claimed свидетельства
	TTLDays    int
	// FavorableValues — значения, выгодные отелю: к утверждению отельера о них применяется асимметрия.
	FavorableValues []string
	// SignificantDiff — для чисел: расхождение меньше этого значения считается «одинаково».
	SignificantDiff float64
	// ConflictSteps и ConflictTheta — правила конфликта (см. TruthParams); 0 — значения по умолчанию.
	ConflictSteps    int
	ConflictTheta    float64
	ConflictEntities []string // сущности, у которых ищутся конфликты; пусто — у всех
	Strategies       []Strategy
	// Category — как характеристика уровня фасада читается в категории номеров (проекция по размещению);
	// nil — на категорию не переносится.
	Category *CategoryRule
	// AskHotelier — вопрос отельеру, если расчёта нет или он неуверен. AskGuest — вопрос гостю в анкете после проживания.
	AskHotelier string
	AskGuest    string
	// Origin — откуда в реестре характеристика: пусто — написана вручную, OriginText — добавлена текстом
	// (collect add-characteristic); OriginText — исходная фраза, AddedAt — дата добавления (YYYY-MM-DD).
	Origin     string
	OriginText string
	AddedAt    string
}

// CategoryRule — правило переноса характеристики с фасадов на категорию номеров. Категория получает
// собственное значение (поле ValueField свидетельства), вероятность Target и гарантию, см. AssessCategory.
type CategoryRule struct {
	Values     []string // значения у категории; у ступенчатого enum — по порядку
	Ordered    bool
	ValueField string // поле значения в claimed свидетельства категории
	// Target — значение, которого ищет гость (вид есть, тихо).
	Target string
	// FloorField — поле факта фасада с этажом, с которого выполняется Target (sea_view: min_floor);
	// ниже этого этажа и при пустом значении — последнее из Values. Пусто — значение берётся из поля
	// значения самой характеристики и от этажа не зависит.
	FloorField      string
	FavorableValues []string
	ConflictSteps   int
	ConflictTheta   float64
	// ConflictThetaHotelier — пониженный θ, только если в споре участвует утверждение отельера (название категории);
	// иначе действует ConflictTheta (по умолчанию 1,0). Гости и расчёт без отельера пониженным порогом не спорят.
	ConflictThetaHotelier float64
}

// FacadeValueAt читает значение характеристики у фасада на этаже из claimed факта фасада.
// valueField — поле значения характеристики. Нет нужного поля — значение неизвестно.
func (r CategoryRule) FacadeValueAt(claimed map[string]any, valueField string, floor int) (string, bool) {
	if r.FloorField == "" {
		v, ok := claimed[valueField].(string)
		return v, ok && slices.Contains(r.Values, v)
	}
	raw, present := claimed[r.FloorField]
	if !present {
		return "", false
	}
	min, ok := raw.(float64)
	if !ok || floor < int(min) {
		return r.Values[len(r.Values)-1], true
	}
	return r.Target, true
}

// OriginText — значение Characteristic.Origin для характеристик, добавленных текстом.
const OriginText = "text"

// TTL — срок годности свидетельства.
func (c Characteristic) TTL() time.Duration { return time.Duration(c.TTLDays) * 24 * time.Hour }

// FieldName — поле значения в claimed.
func (c Characteristic) FieldName() string {
	if c.ValueField != "" {
		return c.ValueField
	}
	return DefaultValueField
}

// TruthParams переводит описание в правила движка правды.
func (c Characteristic) TruthParams() TruthParams {
	p := TruthParams{
		ValueField: c.FieldName(), ConflictSteps: c.ConflictSteps, ConflictMinSide: c.ConflictTheta,
		SignificantDiff: c.SignificantDiff, FavorableValues: c.FavorableValues, ConflictEntityTypes: c.ConflictEntities,
	}
	switch c.ValueType {
	case ValueBool, ValueEnum:
		p.Kind = TruthEnum
		if c.Ordered {
			p.Order = c.Values
		}
	case ValueNumber:
		p.Kind = TruthNumber
	default:
		p.Kind = TruthSingle
	}
	if r := c.Category; r != nil {
		cp := TruthParams{
			Kind: TruthEnum, ValueField: r.ValueField, ConflictSteps: r.ConflictSteps, ConflictMinSide: r.ConflictTheta, ConflictMinSideHotelier: r.ConflictThetaHotelier,
			FavorableValues: r.FavorableValues,
		}
		if r.Ordered {
			cp.Order = r.Values
		}
		p.ByEntity = map[string]TruthParams{EntityRoomCategory: cp}
	}
	return p
}

// Registry — реестр характеристик в порядке описания.
type Registry struct {
	items []Characteristic
	index map[string]int
}

// NewRegistry собирает реестр; ключи должны быть уникальны. Содержательная проверка — в usecase/registry.
func NewRegistry(cs []Characteristic) (Registry, error) {
	r := Registry{items: append([]Characteristic(nil), cs...), index: make(map[string]int, len(cs))}
	for i, c := range r.items {
		if _, dup := r.index[c.Key]; dup {
			return Registry{}, fmt.Errorf("характеристика %q описана дважды", c.Key)
		}
		r.index[c.Key] = i
	}
	return r, nil
}

// All возвращает характеристики в порядке описания.
func (r Registry) All() []Characteristic { return append([]Characteristic(nil), r.items...) }

// Get возвращает характеристику по ключу.
func (r Registry) Get(key string) (Characteristic, bool) {
	i, ok := r.index[key]
	if !ok {
		return Characteristic{}, false
	}
	return r.items[i], true
}

// Rules возвращает правила движка правды по ключам характеристик.
func (r Registry) Rules() map[string]TruthParams {
	out := make(map[string]TruthParams, len(r.items))
	for _, c := range r.items {
		out[c.Key] = c.TruthParams()
	}
	return out
}

// StrategyFor возвращает стратегию характеристики по инструменту и варианту.
func (r Registry) StrategyFor(key, tool, variant string) (Strategy, bool) {
	c, ok := r.Get(key)
	if !ok {
		return Strategy{}, false
	}
	for _, s := range c.Strategies {
		if s.Tool == tool && s.Variant == variant {
			return s, true
		}
	}
	return Strategy{}, false
}

// ToolUse — характеристика, в которой участвует инструмент.
type ToolUse struct {
	Characteristic Characteristic
	Strategies     []Strategy // стратегии этой характеристики, использующие инструмент
}

// ToolUses перечисляет характеристики, которые получает инструмент, в порядке описания.
func (r Registry) ToolUses(tool string) []ToolUse {
	var out []ToolUse
	for _, c := range r.items {
		var ss []Strategy
		for _, s := range c.Strategies {
			if s.Tool == tool {
				ss = append(ss, s)
			}
		}
		if len(ss) > 0 {
			out = append(out, ToolUse{Characteristic: c, Strategies: ss})
		}
	}
	return out
}

package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Состояния характеристики у отеля, по которым агент решает, действовать ли.
const (
	StatusOK        = "ok"        // свежо и уверенно: агент пропускает
	StatusMissing   = "missing"   // факта нет
	StatusStale     = "stale"     // свидетельство старше срока годности
	StatusDisputed  = "disputed"  // открытый конфликт
	StatusWeak      = "weak"      // weak_disagreement: «против» есть, но слабее порога конфликта
	StatusUncertain = "uncertain" // уверенность ниже FreshConfidence
)

const entityDistrict = "district"

// State — состояние одной характеристики у отеля.
type State struct {
	Key        string
	Status     string
	Reason     string
	Confidence *float64
	Value      string // значение основного факта, укороченное
	Facades    *FacadeSummary
}

// FacadeSummary — свёртка фактов по фасадам.
type FacadeSummary struct {
	N         int
	Values    map[string]int
	Weak      int
	Conflicts int
	Stale     int
}

// Needs — характеристику надо считать заново.
func (s State) Needs() bool { return s.Status != StatusOK }

// Assess определяет состояние характеристики у отеля по его фактам.
func Assess(c domain.Characteristic, h HotelInfo, facts []domain.Fact) State {
	st := State{Key: c.Key}
	var primary *domain.Fact
	var weak, conflicts, stale int
	for i := range facts {
		f := &facts[i]
		if f.Key != c.Key {
			continue
		}
		switch f.EntityType {
		case domain.EntityHotel:
			if f.EntityID == strconv.FormatInt(h.ID, 10) {
				primary = f
			}
		case entityDistrict:
			if f.EntityID == h.District {
				primary = f
			}
		case domain.EntityFacade:
			if st.Facades == nil {
				st.Facades = &FacadeSummary{Values: map[string]int{}}
			}
			st.Facades.N++
			st.Facades.Values[fmt.Sprint(f.Value[c.FieldName()])]++
			if f.WeakDisagreement {
				st.Facades.Weak++
			}
			if f.Conflict != nil {
				st.Facades.Conflicts++
			}
			if f.Stale {
				st.Facades.Stale++
			}
		}
		if f.WeakDisagreement {
			weak++
		}
		if f.Conflict != nil {
			conflicts++
		}
		if f.Stale {
			stale++
		}
	}
	if primary == nil {
		st.Status, st.Reason = StatusMissing, "факта нет"
		return st
	}
	conf := math.Round(primary.Confidence*100) / 100
	st.Confidence = &conf
	st.Value = compactValue(primary.Value)
	switch {
	case stale > 0:
		st.Status, st.Reason = StatusStale, fmt.Sprintf("устаревших фактов: %d", stale)
	case conflicts > 0:
		st.Status, st.Reason = StatusDisputed, fmt.Sprintf("открытых конфликтов: %d", conflicts)
	case weak > 0:
		st.Status, st.Reason = StatusWeak, fmt.Sprintf("слабое расхождение источников (weak_disagreement): %d", weak)
	case primary.Confidence < FreshConfidence:
		st.Status, st.Reason = StatusUncertain, fmt.Sprintf("уверенность %.2f ниже %.2f", primary.Confidence, FreshConfidence)
	default:
		st.Status, st.Reason = StatusOK, fmt.Sprintf("свежо, уверенность %.2f, без устаревших, конфликтов и слабых расхождений", primary.Confidence)
	}
	return st
}

// AskReason возвращает причину вопроса отельеру или пустую строку: вопрос создаётся, только если у характеристики
// есть ask_hotelier, а уверенности нет или она ниже AskConfidence.
func AskReason(c domain.Characteristic, st State) string {
	if c.AskHotelier == "" {
		return ""
	}
	switch {
	case st.Confidence == nil:
		return "данных нет"
	case *st.Confidence < AskConfidence:
		return fmt.Sprintf("уверенность %.2f ниже %.2f", *st.Confidence, AskConfidence)
	}
	return ""
}

// ValueScore — ценность вопроса: пока только неуверенность (спрос и влияние — бюджет этапа 5).
func ValueScore(st State) float64 {
	if st.Confidence == nil {
		return 1
	}
	return math.Round((1-*st.Confidence)*100) / 100
}

// View — состояние в виде JSON-объекта для журнала и ответов инструментов (без времени и идентификаторов).
func (s State) View() map[string]any {
	m := map[string]any{"key": s.Key, "status": s.Status, "reason": s.Reason}
	if s.Confidence != nil {
		m["confidence"] = *s.Confidence
		m["value"] = s.Value
	}
	if f := s.Facades; f != nil {
		keys := make([]string, 0, len(f.Values))
		for k := range f.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		vals := map[string]int{}
		for _, k := range keys {
			vals[k] = f.Values[k]
		}
		m["facades"] = map[string]any{"n": f.N, "values": vals, "weak": f.Weak, "conflicts": f.Conflicts, "stale": f.Stale}
	}
	return m
}

func compactValue(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	r := []rune(string(b))
	if len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return string(r)
}

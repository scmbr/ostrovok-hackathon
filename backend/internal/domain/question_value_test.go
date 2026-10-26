package domain

import (
	"math"
	"testing"
)

func TestUncertainty(t *testing.T) {
	cases := []struct {
		name           string
		has            bool
		c              float64
		k              int
		conflict, weak bool
		want           float64
	}{
		{"нет данных", false, 0, 3, false, false, 1},
		{"bool, уверенность 0,5 — полная неуверенность", true, 0.5, 2, false, false, 1},
		{"bool, 0,9", true, 0.9, 2, false, false, 0.2},
		{"enum из трёх, 0,85: шкала растянута", true, 0.85, 3, false, false, 0.225},
		{"конфликт поднимает", true, 0.9, 2, true, false, 0.5},
		{"weak_disagreement поднимает", true, 0.9, 2, false, true, 0.35},
		{"потолок 1", true, 0.6, 2, true, true, 1},
		{"уверенность ниже 1/k не уходит за 1", true, 0.2, 2, false, false, 1},
	}
	for _, c := range cases {
		if got := Uncertainty(c.has, c.c, c.k, c.conflict, c.weak); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: %.4f, ожидалось %.4f", c.name, got, c.want)
		}
	}
}

func TestSimulateAnswer(t *testing.T) {
	// При 50/50 ответ с надёжностью r задаёт значение с уверенностью r.
	o := SimulateAnswer(FactState{Value: "false", Confidence: 0.5}, "true", 0.8)
	if o.Value != "true" || math.Abs(o.Confidence-0.8) > 1e-6 {
		t.Fatalf("при 50/50: %+v", o)
	}
	// Подтверждение повышает уверенность, значение не меняется.
	o = SimulateAnswer(FactState{Value: "true", Confidence: 0.7}, "true", 0.8)
	if o.Value != "true" || o.Confidence <= 0.7 {
		t.Fatalf("подтверждение: %+v", o)
	}
	// Слабый ответ против сильного факта значение не меняет.
	o = SimulateAnswer(FactState{Value: "quiet", Confidence: 0.95}, "loud", 0.6)
	if o.Value != "quiet" || o.Confidence >= 0.95 {
		t.Fatalf("слабый ответ против сильного факта: %+v", o)
	}
}

func TestFactImpact(t *testing.T) {
	r := HotelierAnswerReliability(DefaultTrust, false) // 0,8
	answers := []string{"true", "false"}
	if got := FactImpact(false, FactState{}, answers, r); got != 1 {
		t.Errorf("нет данных: %v, ожидалась 1", got)
	}
	if got := FactImpact(true, FactState{Value: "true", Confidence: 0.55}, answers, r); got != 1 {
		t.Errorf("0,55: ответ переводит через 0,6: %v", got)
	}
	// Уверенность 0,99: ответ с r=0,8 не сдвигает значение и не опускает уверенность ниже 0,75.
	if got := FactImpact(true, FactState{Value: "true", Confidence: 0.99}, answers, r); got != 0 {
		t.Errorf("0,99: ответ ничего не меняет: %v", got)
	}
	// Уверенность 0,8: ответ против опускает её ниже 0,75 — влияние есть.
	if got := FactImpact(true, FactState{Value: "true", Confidence: 0.8}, answers, r); got != 1 {
		t.Errorf("0,8: ответ против переводит через 0,75: %v", got)
	}
}

// Вопрос уровня отеля должен уходить: категорий у него нет, но влияние на факт даёт ненулевую ценность.
func TestHotelLevelQuestionCanBeSent(t *testing.T) {
	impact := Impact(FactImpact(false, FactState{}, nil, 0.8), 0, 0)
	v := QuestionValue(NormalizedDemand(7, 10), Uncertainty(false, 0, 2, false, false), impact)
	if v < QuestionSendThreshold {
		t.Fatalf("вопрос без данных при заметном спросе должен уходить: %.3f < %.2f", v, QuestionSendThreshold)
	}
}

func TestImpactTakesLarger(t *testing.T) {
	if got := Impact(0, 1, 4); math.Abs(got-0.25) > 1e-9 {
		t.Errorf("доля категорий: %v", got)
	}
	if got := Impact(1, 1, 4); got != 1 {
		t.Errorf("влияние на факт: %v", got)
	}
	if got := Impact(0, 0, 0); got != 0 {
		t.Errorf("нет категорий и факт не меняется: %v", got)
	}
}

func TestQuestionValueLowDemand(t *testing.T) {
	if v := QuestionValue(NormalizedDemand(1, 20), 1, 1); v >= QuestionSendThreshold {
		t.Errorf("малый спрос — ниже порога: %.3f", v)
	}
	if NormalizedDemand(0, 5) != 0 || NormalizedDemand(3, 0) != 0 {
		t.Error("нулевой спрос")
	}
}

package domain

import "math"

// Ценность вопроса отельеру: спрос × неуверенность × влияние. Ниже порога вопрос не отправляется, а копится;
// отправленных вопросов на отель не больше бюджета за окно. Пороги — константы, описание — docs/hotelier.md.
const (
	// QuestionSendThreshold — минимальная ценность, при которой вопрос уходит отельеру.
	QuestionSendThreshold = 0.2
	// QuestionBudget — вопросов, отправленных отельеру за QuestionBudgetDays дней.
	QuestionBudget     = 3
	QuestionBudgetDays = 7
	// ConflictUncertaintyBoost и WeakDisagreementBoost — надбавки к неуверенности за открытый конфликт
	// и за слабое расхождение источников; итог неуверенности не больше 1.
	ConflictUncertaintyBoost = 0.3
	WeakDisagreementBoost    = 0.15
)

// Отметки уверенности совпадают с порогами агента-сборщика: ниже 0,6 — спрашиваем, от 0,75 — факт свежий и надёжный.
// Переход через отметку меняет решение (выдача, объяснение), поэтому считается влиянием ответа.
const (
	AskConfidenceMark   = 0.6
	FreshConfidenceMark = 0.75
)

var confidenceMarks = []float64{AskConfidenceMark, FreshConfidenceMark}

// Персоны имитированных отельеров (hotels.hotelier_persona, hotelier_questions.persona).
const (
	PersonaHonest       = "honest"
	PersonaLazy         = "lazy"
	PersonaEmbellishing = "embellishing"
)

// Uncertainty — неуверенность в характеристике, 0..1: u = (1 − c) / (1 − 1/k), k — число значений
// (у bool 2). Нет данных — 1. Открытый конфликт и weak_disagreement добавляют надбавку, итог не выше 1.
func Uncertainty(hasFact bool, confidence float64, values int, conflict, weak bool) float64 {
	if !hasFact {
		return 1
	}
	k := float64(max(values, 2))
	u := (1 - confidence) / (1 - 1/k)
	if conflict {
		u += ConflictUncertaintyBoost
	}
	if weak {
		u += WeakDisagreementBoost
	}
	return math.Min(math.Max(u, 0), 1)
}

// FactState — текущее состояние факта для оценки влияния ответа.
type FactState struct {
	Value      string
	Confidence float64
}

// AnswerOutcome — каким станет факт, если отельер ответит значением Answer.
type AnswerOutcome struct {
	Answer     string
	Value      string
	Confidence float64
}

// SimulateAnswer прикидывает факт после ответа отельера значением answer с надёжностью r (после репутации и
// асимметрии). Приближение движка правды для bool/enum: текущее значение против «остальных» в логарифме шансов;
// ответ в пользу значения прибавляет ln(r/(1−r)), против — вычитает; ушли в минус — значением становится ответ.
func SimulateAnswer(cur FactState, answer string, r float64) AnswerOutcome {
	r = math.Min(math.Max(r, MinReliability), MaxReliability)
	c := math.Min(math.Max(cur.Confidence, MinFactConfidence), MaxFactConfidence)
	s := math.Log(c / (1 - c))
	l := math.Log(r / (1 - r))
	out := AnswerOutcome{Answer: answer, Value: cur.Value}
	if answer == cur.Value {
		s += l
	} else {
		s -= l
		if s < 0 {
			s, out.Value = -s, answer
		}
	}
	out.Confidence = math.Min(math.Max(1/(1+math.Exp(-s)), MinFactConfidence), MaxFactConfidence)
	return out
}

// Changes сообщает, меняет ли исход факт: другое значение или уверенность по другую сторону отметки.
func (o AnswerOutcome) Changes(cur FactState) bool {
	if o.Value != cur.Value {
		return true
	}
	for _, m := range confidenceMarks {
		if (cur.Confidence < m) != (o.Confidence < m) {
			return true
		}
	}
	return false
}

// FactImpact — влияние ответа на факт: 1, если хоть один возможный ответ меняет значение или переводит
// уверенность через отметку 0,6 или 0,75; иначе 0. Нет данных — 1 (любой ответ заполнит пробел).
// Без этого вопросы по характеристикам уровня отеля не уходили бы никогда: в категориях им нечего менять.
func FactImpact(hasFact bool, cur FactState, answers []string, r float64) float64 {
	if !hasFact {
		return 1
	}
	for _, a := range answers {
		if SimulateAnswer(cur, a, r).Changes(cur) {
			return 1
		}
	}
	return 0
}

// Impact — влияние ответа: на факт (FactImpact) или, у характеристик фасада, доля категорий отеля, у которых ответ
// меняет вероятность искомого значения (AssessCategory). Берётся большее.
func Impact(factImpact float64, categoriesChanged, categoriesTotal int) float64 {
	share := 0.0
	if categoriesTotal > 0 {
		share = float64(categoriesChanged) / float64(categoriesTotal)
	}
	return math.Max(factImpact, share)
}

// NormalizedDemand — спрос 0..1: доля от максимального спроса в регионе.
func NormalizedDemand(count, maxCount int) float64 {
	if count <= 0 || maxCount <= 0 {
		return 0
	}
	return math.Min(float64(count)/float64(maxCount), 1)
}

// QuestionValue — ценность вопроса: спрос × неуверенность × влияние, каждый множитель 0..1.
func QuestionValue(demand, uncertainty, impact float64) float64 {
	return demand * uncertainty * impact
}

// HotelierAnswerReliability — надёжность ответа отельера в симуляции: база × (0,5 + репутация) × асимметрия
// для выгодного отелю значения. Как в движке правды, без поправки свежести (ответ только что получен).
func HotelierAnswerReliability(trust float64, favorable bool) float64 {
	r := HotelierBaseReliability * (0.5 + trust)
	if favorable {
		r *= HotelierAsymmetry
	}
	return math.Min(math.Max(r, MinReliability), MaxReliability)
}

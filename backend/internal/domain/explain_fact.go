package domain

import (
	"fmt"
	"math"
	"strings"
)

// Роли свидетельства в сложении (EvidenceExplanation.Role).
const (
	RoleFor      = "for"       // за итоговое значение факта
	RoleAgainst  = "against"   // против итогового значения
	RoleIgnoredD = "dependent" // зависимый источник: из группы независимости взято сильнейшее
)

// EvidenceExplanation — одно свидетельство в составе факта: цепочка надёжности и вклад.
type EvidenceExplanation struct {
	Evidence Evidence
	// Chain: база → репутация → асимметрия → свежесть → ограничение [0,05; 0,97] → R.
	Chain ReliabilityChain
	// Logit — ln(R / (1 − R)), но не меньше нуля: надёжность не выше 0,5 информации не несёт.
	Logit float64
	// Role — за / против / зависимый источник; Contribution — подписанный вклад в логарифм шансов (против — минус).
	Role         string
	Contribution float64
	// Group — группа независимости (пусто, если свидетельство ни от кого не зависит).
	Group string
	// ZeroWeight — вклад ≈ 0 при надёжности около 0,5: свидетельство ничего не добавляет (чаще всего из-за репутации).
	ZeroWeight bool
}

// FactExplanation — факт и всё, что дало его уверенность. Только чтение: считает ту же функцию движка, что и сборка факта.
type FactExplanation struct {
	Fact     Fact
	Evidence []EvidenceExplanation
	// S — сумма вкладов (для bool / enum — логарифм шансов значения факта); Sigma — σ(S).
	S, Sigma float64
	// Theta — порог конфликта θ, применённый к этому факту; ThetaLowered — он пониженный (conflict_theta_hotelier),
	// потому что в споре участвует отельер. ValueField — поле значения в claimed.
	Theta        float64
	ThetaLowered bool
	ValueField   string
	// Summary — «уверенность 0,87: кадастр +, отельер +, 2 гостя −».
	Summary string
}

// ExplainFact собирает факт из свидетельств es (AssembleFact) и раскладывает его уверенность по свидетельствам.
func ExplainFact(es []Evidence, c TruthContext) (FactExplanation, bool) {
	f, ws, p, ok := assembleDetailed(es, c)
	if !ok {
		return FactExplanation{}, false
	}
	x := FactExplanation{Fact: f, Theta: p.minSideFor(ws), ValueField: p.ValueField}
	x.ThetaLowered = p.ConflictMinSideHotelier > 0 && x.Theta == p.ConflictMinSideHotelier && x.Theta != p.conflictMinSide()
	for i, w := range ws {
		e := EvidenceExplanation{Evidence: w.ev, Chain: reliabilityChain(w.ev, p, c), Logit: logOdds(w.r),
			Contribution: f.Contributions[i].Value, Group: independenceGroup(w.ev)}
		switch w.role {
		case roleFor:
			e.Role = RoleFor
		case roleAgainst:
			e.Role = RoleAgainst
		default:
			e.Role = RoleIgnoredD
		}
		e.ZeroWeight = e.Role != RoleIgnoredD && math.Abs(e.Contribution) < 0.005 && e.Chain.R <= 0.5+1e-6
		x.S += e.Contribution
		x.Evidence = append(x.Evidence, e)
	}
	x.Sigma = sigmoid(x.S)
	x.Summary = summarize(f.Confidence, x.Evidence)
	return x, true
}

// sourceWord — источник свидетельства словами для сводки.
func sourceWord(e Evidence) string {
	switch e.AuthorType {
	case AuthorHotelier:
		return "отельер"
	case AuthorGuest:
		return "гость"
	}
	switch e.Source {
	case SourceBCNNoise:
		return "карта шума"
	case SourceOSM:
		return "OSM"
	case SourceComputed:
		return "расчёт"
	}
	return e.Source
}

// summarize — «уверенность 0,87: кадастр +, отельер +, 2 гостя −». Знак — сторона свидетельства относительно итога;
// вклад ≈ 0 или зависимый источник — «≈0».
func summarize(conf float64, es []EvidenceExplanation) string {
	type key struct{ word, sign string }
	counts := map[key]int{}
	var order []key
	for _, e := range es {
		sign := "+"
		switch {
		case e.Role == RoleIgnoredD || math.Abs(e.Contribution) < 0.005:
			sign = "≈0"
		case e.Role == RoleAgainst:
			sign = "−"
		}
		k := key{sourceWord(e.Evidence), sign}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		w := k.word
		if n := counts[k]; n > 1 {
			if k.word == "гость" {
				w = fmt.Sprintf("%d %s", n, plural3(n, "гость", "гостя", "гостей"))
			} else {
				w = fmt.Sprintf("%d× %s", n, k.word)
			}
		}
		parts = append(parts, w+" "+k.sign)
	}
	return fmt.Sprintf("уверенность %s: %s", comma2(conf), strings.Join(parts, ", "))
}

func plural3(n int, one, few, many string) string {
	m10, m100 := n%10, n%100
	switch {
	case m10 == 1 && m100 != 11:
		return one
	case m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20):
		return few
	}
	return many
}

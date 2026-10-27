package domain

import "fmt"

// Репутация автора-человека: доля его утверждений, подтверждённых независимым надёжным свидетельством.
// Описание и пример — docs/truth.md, раздел «Репутация».

// MinJudgeConfidence — наименьшая уверенность факта, собранного из свидетельств без самого автора, при которой факт
// служит судьёй его утверждению. Слабый факт (расчёт по OSM, категория с r = 0,6) судьёй не служит. Сила расхождения не
// важна: любое несовпадение значения считается расхождением.
const MinJudgeConfidence = 0.8

// Verdict — итог сравнения одного утверждения автора с судьёй.
type Verdict struct {
	// Counted — утверждение сравнимо: значение bool / enum и есть судья (факт без автора с уверенностью не ниже порога).
	Counted bool
	// Agree — значение утверждения совпало со значением судьи.
	Agree bool
	// Against — значение судьи, с которым сравнивали (пусто, если Counted = false).
	Against string
	// ByConfidence — уверенность судьи.
	ByConfidence float64
}

// JudgeClaim сравнивает утверждение claim с фактом, собранным из свидетельств others — всех активных свидетельств той же
// сущности и характеристики, кроме свидетельств этого автора, — той же функцией движка правды (AssembleFact), с
// репутацией остальных авторов из c.Trust. Судья есть, если уверенность такого факта не ниже MinJudgeConfidence.
// Числа и структуры не сравниваются («числа не голосуют»). Своё же прежнее свидетельство судьёй не служит.
//
// Судья не зависит от репутации самого автора, поэтому репутацию можно считать до сборки факта с его свидетельством.
func JudgeClaim(claim Evidence, others []Evidence, c TruthContext) Verdict {
	p := c.params(claim.Key, claim.EntityType)
	if p.Kind != TruthEnum {
		return Verdict{}
	}
	mine := claimedString(claim, p)
	if mine == "" {
		return Verdict{}
	}
	var rest []Evidence
	for _, o := range others {
		if (o.AuthorType == claim.AuthorType && o.AuthorID == claim.AuthorID) || claimedString(o, p) == "" {
			continue
		}
		rest = append(rest, o)
	}
	f, ok := AssembleFact(rest, c)
	if !ok || f.Confidence+1e-9 < MinJudgeConfidence {
		return Verdict{}
	}
	theirs := fmt.Sprint(f.Value[p.ValueField])
	return Verdict{Counted: true, Agree: theirs == mine, Against: theirs, ByConfidence: f.Confidence}
}

// TrustFromCounts — репутация (совпадения + 1) / (всего + 2); без сравнимых утверждений — DefaultTrust.
func TrustFromCounts(agree, total int) float64 {
	return float64(agree+1) / float64(total+2)
}

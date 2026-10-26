package domain

// Репутация автора-человека: доля его утверждений, подтверждённых независимым надёжным свидетельством.
// Описание и пример — docs/truth.md, раздел «Репутация».

// MinIndependentReliability — наименьшая надёжность r независимого свидетельства (другого автора или источника),
// с которым сравнивается утверждение автора. Слабое свидетельство (расчёт по OSM, категория с r = 0,6) судьёй не служит.
// Сила расхождения не важна: любое несовпадение значения считается расхождением.
const MinIndependentReliability = 0.8

// Verdict — итог сравнения одного утверждения автора с независимыми свидетельствами.
type Verdict struct {
	// Counted — утверждение сравнимо: значение bool / enum и есть независимое свидетельство с r ≥ порога.
	Counted bool
	// Agree — значение совпало с сильнейшим независимым свидетельством.
	Agree bool
	// Against — значение, с которым сравнивали (пусто, если Counted = false).
	Against string
	// ByReliability — надёжность этого свидетельства.
	ByReliability float64
}

// JudgeClaim сравнивает утверждение claim с другими активными свидетельствами той же сущности и характеристики.
// Независимы свидетельства другого автора (тип и идентификатор отличаются). Из них берётся сильнейшее с r ≥
// MinIndependentReliability; числа и структуры не сравниваются («числа не голосуют»).
func JudgeClaim(claim Evidence, others []Evidence, c TruthContext) Verdict {
	p := c.params(claim.Key, claim.EntityType)
	if p.Kind != TruthEnum {
		return Verdict{}
	}
	mine := claimedString(claim, p)
	if mine == "" {
		return Verdict{}
	}
	var best *Evidence
	for k := range others {
		o := &others[k]
		if o.AuthorType == claim.AuthorType && o.AuthorID == claim.AuthorID {
			continue
		}
		if o.Reliability < MinIndependentReliability || claimedString(*o, p) == "" {
			continue
		}
		if best == nil || o.Reliability > best.Reliability {
			best = o
		}
	}
	if best == nil {
		return Verdict{}
	}
	theirs := claimedString(*best, p)
	return Verdict{Counted: true, Agree: theirs == mine, Against: theirs, ByReliability: best.Reliability}
}

// TrustFromCounts — репутация (совпадения + 1) / (всего + 2); без сравнимых утверждений — DefaultTrust.
func TrustFromCounts(agree, total int) float64 {
	return float64(agree+1) / float64(total+2)
}

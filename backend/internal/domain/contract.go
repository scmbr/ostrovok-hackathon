package domain

// Контракт ожиданий: список важного для этого гостя по выбранному варианту номера, с честной степенью уверенности
// по каждому пункту. Строится кодом из оценок поиска (WishEval). Описание — docs/compare.md.

// Виды пунктов контракта.
const (
	ContractGuaranteed    = "guaranteed"     // гарантировано
	ContractChance        = "chance"         // шанс N %
	ContractWishPending   = "wish_pending"   // с пожеланием, если отель подтвердит
	ContractWishConfirmed = "wish_confirmed" // с пожеланием, отель гарантирует пожелания
	ContractWishRefused   = "wish_refused"   // с пожеланием, отель ответил, что не гарантирует
	ContractUncertain     = "uncertain"      // под вопросом
	ContractUnknown       = "unknown"        // неизвестно
	ContractMismatch      = "mismatch"       // не совпадёт
)

// ContractLine — пункт контракта.
type ContractLine struct {
	Key        string
	Title      string
	Importance Importance
	Kind       string
	// Probability — вероятность выполнения (для guaranteed 1, для unknown 0).
	Probability float64
	Confidence  float64
	Text        string
	// Source — семейства источников («расчёт + отельер»); пусто, если данных нет.
	Source string
	// Conflict — открытый конфликт по пункту.
	Conflict string
	// CanAsk — у пункта есть смысл спросить отель: он неуверенный или неизвестный.
	CanAsk bool
}

// ContractLines строит пункты контракта по варианту. withWish — гость бронирует с пожеланием: у фасадных пунктов
// берётся вероятность с пожеланием и статус ответа отеля.
func ContractLines(reg Registry, v Variant, withWish bool) []ContractLine {
	out := make([]ContractLine, 0, len(v.Evals))
	for _, e := range v.Evals {
		c, ok := reg.Get(e.Key)
		if !ok {
			continue
		}
		out = append(out, contractLine(c, e, withWish))
	}
	return out
}

func contractLine(c Characteristic, e WishEval, withWish bool) ContractLine {
	title := c.Title
	if title == "" {
		title = c.Key
	}
	l := ContractLine{Key: e.Key, Title: title, Importance: e.Importance, Confidence: e.Confidence, Conflict: e.Conflict,
		Source: evidenceSources(e.Sources)}
	switch e.Status {
	case StatusMatch:
		l.Kind, l.Probability = ContractGuaranteed, 1
		l.Text = "гарантировано"
		if v := displayValue(c, e); v != "" {
			l.Text += " (" + v + ")"
		}
	case StatusChance:
		l.Kind, l.Probability = ContractChance, e.P
		l.Text = "шанс " + percent(e.P)
		if withWish && e.Wish != nil {
			l.Probability = e.Wish.Probability
			switch {
			case e.Wish.HotelConfirmed:
				l.Kind = ContractWishConfirmed
				l.Text = "с пожеланием: отель гарантирует пожелания (шанс " + percent(l.Probability) + ")"
			case e.Wish.HotelRefused:
				l.Kind = ContractWishRefused
				l.Text = "с пожеланием: отель не гарантирует пожелания (шанс " + percent(l.Probability) + ")"
			default:
				l.Kind = ContractWishPending
				l.Text = "с пожеланием, если отель подтвердит (шанс " + percent(l.Probability) + ")"
			}
		} else if e.Wish != nil {
			l.Text += "; с пожеланием выше, если отель подтвердит"
		}
	case StatusNear:
		l.Kind, l.Probability = ContractUncertain, e.P
		l.Text = "на границе"
		if e.Caveat != "" {
			l.Text += " — " + e.Caveat
		}
	case StatusMismatch:
		l.Kind = ContractMismatch
		l.Text = "не совпадёт"
		if v := displayValue(c, e); v != "" {
			l.Text += " (" + v + ")"
		}
	case StatusUncertain:
		l.Kind = ContractUncertain
		l.Text = "под вопросом"
		if e.Caveat != "" {
			l.Text += " — " + e.Caveat
		}
	default:
		l.Kind = ContractUnknown
		l.Text = "неизвестно — спросим отель"
		l.Source = ""
	}
	l.CanAsk = l.Kind == ContractUncertain || l.Kind == ContractUnknown
	return l
}

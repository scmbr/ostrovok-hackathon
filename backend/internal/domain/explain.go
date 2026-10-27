package domain

import (
	"fmt"
	"slices"
	"strings"
)

// Объяснение выдачи строится из структуры WishEval, а не из текста модели: каждое число и каждый статус
// берутся из оценки. LLM может только пересказать готовый текст (usecase/dialog проверяет числа и названия).

// ExplainWish — одна строка объяснения по желанию: что совпало, что под вопросом и откуда известно.
func ExplainWish(c Characteristic, e WishEval) string {
	title := c.Title
	if title == "" {
		title = c.Key
	}
	var b strings.Builder
	b.WriteString(title)
	switch e.Status {
	case StatusMatch:
		b.WriteString(": совпало")
		if v := displayValue(c, e); v != "" {
			b.WriteString(" (" + v + ")")
		}
		if e.Guarantee {
			b.WriteString(", в категории гарантировано")
		}
	case StatusChance:
		fmt.Fprintf(&b, ": шанс %s", percent(e.P))
		if e.Wish != nil {
			b.WriteString("; " + wishPhrase(*e.Wish))
		}
	case StatusUncertain:
		b.WriteString(": под вопросом")
		// Оговорка «уверенность N %» повторяла бы то, что строка и так показывает ниже.
		if e.Caveat != "" && !strings.HasPrefix(e.Caveat, "уверенность") {
			b.WriteString(" — " + e.Caveat)
		}
	case StatusNear:
		b.WriteString(": на границе")
		if e.Caveat != "" {
			b.WriteString(" — " + e.Caveat)
		}
	case StatusMismatch:
		b.WriteString(": не совпало")
		if v := displayValue(c, e); v != "" {
			b.WriteString(" (" + v + ")")
		}
	default:
		b.WriteString(": данных нет, спросим отель")
	}
	if e.Status != StatusUnknown && e.Confidence > 0 {
		fmt.Fprintf(&b, "; уверенность %s", percent(e.Confidence))
		if src := evidenceSources(e.Sources); src != "" {
			b.WriteString(", " + src)
		}
		if lowTrustHotelier(e.Sources) {
			b.WriteString("; " + LowTrustNote)
		}
	}
	return b.String()
}

// LowTrustNote — пометка к утверждению отельера, чья репутация ниже 0,5.
const LowTrustNote = "со слов отеля; часть прошлых утверждений расчёт не подтвердил"

// lowTrustHotelier — среди источников есть отельер с репутацией ниже нейтральной.
func lowTrustHotelier(src []EvidenceSource) bool {
	for _, s := range src {
		if s.AuthorType == AuthorHotelier && s.AuthorTrust > 0 && s.AuthorTrust < DefaultTrust {
			return true
		}
	}
	return false
}

func displayValue(c Characteristic, e WishEval) string {
	if v, ok := toFloat(e.Value); ok && c.ValueType == ValueNumber {
		return formatNum(v) + unitSuffix(c)
	}
	return ""
}

func wishPhrase(w VariantWish) string {
	p := percent(w.Probability)
	if w.MinFloor > 0 {
		return fmt.Sprintf("пожелание к брони: этаж %d+, шанс с ним %s", w.MinFloor, p)
	}
	return "пожелание к брони: подходящая сторона, шанс с ним " + p
}

func percent(v float64) string { return fmt.Sprintf("%.0f %%", v*100) }

// evidenceSources — человекочитаемые семейства источников свидетельств («расчёт», «отельер», «гости»).
func evidenceSources(src []EvidenceSource) string {
	var names []string
	for _, s := range src {
		var n string
		switch s.AuthorType {
		case "hotelier":
			n = "отельер"
		case "guest":
			n = "гости"
		default:
			n = "расчёт"
		}
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return strings.Join(names, " + ")
}

// HotelExplanation — объяснение выдачи по одному отелю.
type HotelExplanation struct {
	HotelID  int64
	Headline string
	Lines    []string
	// Notes — то, о чём гостю стоит знать: нарушенные must, конфликты в других категориях.
	Notes []string
}

// ExplainHotel строит объяснение лучшего варианта отеля; rank — место в выдаче с 1.
func ExplainHotel(reg Registry, rank int, h HotelResult) HotelExplanation {
	x := HotelExplanation{HotelID: h.HotelID}
	x.Headline = fmt.Sprintf("%d. %s — %s", rank, h.Name, h.Best.Name)
	if h.Best.PriceEUR > 0 {
		x.Headline += fmt.Sprintf(", %d € за ночь", h.Best.PriceEUR)
	}
	for _, e := range h.Best.Evals {
		c, ok := reg.Get(e.Key)
		if !ok {
			continue
		}
		x.Lines = append(x.Lines, ExplainWish(c, e))
	}
	for _, key := range h.Best.Violations {
		title := key
		if c, ok := reg.Get(key); ok {
			title = c.Title
		}
		x.Notes = append(x.Notes, "не выполнено обязательное: "+title)
	}
	for _, oc := range h.OtherConflicts {
		x.Notes = append(x.Notes, fmt.Sprintf("в категории «%s» источники спорят: %s", oc.Category, oc.Description))
	}
	if h.Alt != nil {
		x.Notes = append(x.Notes, fmt.Sprintf("альтернатива: %s, %d € за ночь", h.Alt.Name, h.Alt.PriceEUR))
	}
	return x
}

// Text — объяснение одним блоком.
func (x HotelExplanation) Text() string {
	var b strings.Builder
	b.WriteString(x.Headline)
	for _, l := range x.Lines {
		b.WriteString("\n  • " + l)
	}
	for _, n := range x.Notes {
		b.WriteString("\n  ! " + n)
	}
	return b.String()
}

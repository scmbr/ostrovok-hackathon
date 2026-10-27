package domain

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Умное сравнение: сравниваются лучшие для гостя варианты номера (отель + категория), только по желаниям гостя.
// Всё считается кодом из структур поиска (WishEval, Variant), без LLM. Описание — docs/compare.md.

const (
	// CompareTieScore — оценки вариантов, различающиеся меньше, считаются равными: оценка грубая, вывод
	// «A лучше» не должен прыгать от мелочей.
	CompareTieScore = 0.05
	// MaxCompareSides — сколько вариантов сравниваем за раз.
	MaxCompareSides = 3
	// DayOther — группа сравнения для характеристик без day_part.
	DayOther = "other"
)

// CompareSide — один сравниваемый вариант: отель и лучшая для гостя категория.
type CompareSide struct {
	HotelID int64
	// Label — как называем сторону: название отеля (между отелями) или категории (внутри отеля).
	Label   string
	Variant Variant
	// Conflicts — открытые конфликты других категорий отеля по желаниям гостя.
	Conflicts []VariantConflict
}

// CompareDifference — сравнение одного желания гостя.
type CompareDifference struct {
	Key     string
	Title   string
	DayPart string
	// Same — различие ниже порога значимости: «одинаково».
	Same bool
	// Better — индекс стороны, где лучше; -1, если одинаково.
	Better int
	// Texts — строка объяснения по каждой стороне (статус, значение, уверенность, источник).
	Texts []string
	// Short — короткий статус по каждой стороне («гарантирован», «шанс 25 %»).
	Short []string
	// Importance — важность желания.
	Importance Importance
}

// CompareUncertain — неуверенный пункт стороны, по которому можно уточнить у отеля.
type CompareUncertain struct {
	Side    int
	HotelID int64
	Key     string
	Title   string
	Reason  string
}

// DayGroup — различия одной части дня.
type DayGroup struct {
	Part  string
	Title string
	Keys  []string
}

// Comparison — результат сравнения в порядке подачи: вывод → нарушения → различия → цена компромисса → неуверенное.
type Comparison struct {
	Sides []CompareSide
	// Winner — индекс лучшей стороны; -1 — равноценны.
	Winner  int
	Verdict string
	// Violations — нарушенные must по сторонам (названия характеристик).
	Violations [][]string
	// Differences — существенные различия; Same — «одинаково» (показываются отдельным списком).
	Differences []CompareDifference
	Same        []CompareDifference
	Tradeoff    string
	Uncertain   []CompareUncertain
	Days        []DayGroup
}

// Compare сравнивает 2–3 варианта. wishes — желания гостя в том же порядке, что и в оценках вариантов.
func Compare(reg Registry, wishes []Desire, sides []CompareSide) Comparison {
	cmp := Comparison{Sides: sides, Winner: -1}
	for _, s := range sides {
		var titles []string
		for _, k := range s.Variant.Violations {
			titles = append(titles, wishTitle(reg, k))
		}
		cmp.Violations = append(cmp.Violations, titles)
	}
	for _, w := range wishes {
		c, ok := reg.Get(w.Key)
		if !ok {
			continue
		}
		d := compareWish(c, sides)
		if d.Same {
			cmp.Same = append(cmp.Same, d)
		} else {
			cmp.Differences = append(cmp.Differences, d)
		}
	}
	cmp.Winner, cmp.Verdict = compareVerdict(reg, sides, cmp.Differences)
	cmp.Tradeoff = tradeoff(sides, cmp.Winner, cmp.Differences)
	cmp.Uncertain = uncertain(reg, sides)
	cmp.Days = dayGroups(append(append([]CompareDifference(nil), cmp.Differences...), cmp.Same...))
	return cmp
}

func evalOf(v Variant, key string) (WishEval, bool) {
	for _, e := range v.Evals {
		if e.Key == key {
			return e, true
		}
	}
	return WishEval{}, false
}

// compareWish сравнивает желание по сторонам. Числа — по значениям с порогом significant_diff, остальное — по оценке
// с порогом CompareTieScore.
func compareWish(c Characteristic, sides []CompareSide) CompareDifference {
	d := CompareDifference{Key: c.Key, Title: c.Title, DayPart: c.DayPart, Better: -1}
	if d.Title == "" {
		d.Title = c.Key
	}
	scores := make([]float64, len(sides))
	nums := make([]float64, 0, len(sides))
	for i, s := range sides {
		e, ok := evalOf(s.Variant, c.Key)
		if !ok {
			d.Texts = append(d.Texts, c.Title+": нет оценки")
			d.Short = append(d.Short, "нет оценки")
			continue
		}
		d.Importance = e.Importance
		d.Texts = append(d.Texts, ExplainWish(c, e))
		d.Short = append(d.Short, shortStatus(c, e))
		scores[i] = e.Score
		if v, ok := toFloat(e.Value); ok && c.ValueType == ValueNumber && e.Status != StatusUnknown {
			nums = append(nums, v)
		}
	}
	best, worst := 0, 0
	for i := range scores {
		if scores[i] > scores[best] {
			best = i
		}
		if scores[i] < scores[worst] {
			worst = i
		}
	}
	gap := scores[best] - scores[worst]
	// Числа: одинаково, если значения ближе significant_diff; различие по оценке ниже порога тоже одинаково.
	if len(nums) == len(sides) && c.SignificantDiff > 0 {
		lo, hi := nums[0], nums[0]
		for _, v := range nums {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		if hi-lo < c.SignificantDiff {
			d.Same = true
			return d
		}
	}
	if gap < CompareTieScore {
		d.Same = true
		return d
	}
	d.Better = best
	return d
}

// shortStatus — короткий статус желания в варианте для цены компромисса и таблицы.
func shortStatus(c Characteristic, e WishEval) string {
	switch e.Status {
	case StatusMatch:
		if e.Guarantee {
			return "гарантирован"
		}
		if v := displayValue(c, e); v != "" {
			return "совпало (" + v + ")"
		}
		return "совпало"
	case StatusChance:
		return "шанс ~" + percent(e.P) + withWishSuffix(e)
	case StatusNear:
		return "на границе"
	case StatusMismatch:
		if e.Conflict != "" {
			return "под вопросом (источники спорят)"
		}
		if v := displayValue(c, e); v != "" {
			return "не совпало (" + v + ")"
		}
		return "не совпало"
	case StatusUncertain:
		if e.Wish != nil && e.PWish > 0 {
			return "под вопросом, ~" + percent(e.PWish) + " с пожеланием"
		}
		return "под вопросом"
	}
	return "данных нет"
}

func fulfilledCount(v Variant) int {
	n := 0
	for _, e := range v.Evals {
		if evalFulfilled(e) {
			n++
		}
	}
	return n
}

// compareVerdict — кто лучше: меньше нарушенных must, затем больше выполненных желаний, затем оценка. Оценки ближе
// CompareTieScore — равноценны, решает главное различие (или цена).
func compareVerdict(reg Registry, sides []CompareSide, diffs []CompareDifference) (int, string) {
	if len(sides) == 0 {
		return -1, ""
	}
	order := make([]int, len(sides))
	for i := range order {
		order[i] = i
	}
	type verdictKind int
	const (
		byViolations verdictKind = iota
		byFulfilled
		byScore
		tie
	)
	// decide сообщает, чем сторона a лучше b, или tie.
	decide := func(a, b int) (bool, verdictKind) {
		va, vb := sides[a].Variant, sides[b].Variant
		if len(va.Violations) != len(vb.Violations) {
			return len(va.Violations) < len(vb.Violations), byViolations
		}
		if fa, fb := fulfilledCount(va), fulfilledCount(vb); fa != fb {
			return fa > fb, byFulfilled
		}
		if math.Abs(va.Score-vb.Score) >= CompareTieScore {
			return va.Score > vb.Score, byScore
		}
		return false, tie
	}
	sort.SliceStable(order, func(i, j int) bool {
		win, kind := decide(order[i], order[j])
		return kind != tie && win
	})
	a, b := order[0], order[1]
	_, kind := decide(a, b)
	if kind == tie {
		return -1, tieText(sides, order, diffs)
	}
	return a, winText(reg, sides, a, b, kind == byViolations, diffs)
}

func winText(reg Registry, sides []CompareSide, w, other int, byViol bool, diffs []CompareDifference) string {
	name := sides[w].Label
	if byViol {
		return fmt.Sprintf("Для вас лучше %s, потому что у %s не выполнено обязательное: %s.",
			name, sides[other].Label, joinAnd(violationTitles(reg, sides[other])))
	}
	var why []string
	for _, d := range diffs {
		if d.Better == w {
			why = append(why, strings.ToLower(d.Title)+" — "+d.Short[w])
		}
	}
	if len(why) == 0 {
		return fmt.Sprintf("Для вас лучше %s: выше общая оценка по вашим желаниям.", name)
	}
	return fmt.Sprintf("Для вас лучше %s, потому что %s.", name, strings.Join(why, "; "))
}

func violationTitles(reg Registry, s CompareSide) []string {
	var out []string
	for _, k := range s.Variant.Violations {
		out = append(out, wishTitle(reg, k))
	}
	return out
}

func tieText(sides []CompareSide, order []int, diffs []CompareDifference) string {
	a, b := order[0], order[1]
	head := fmt.Sprintf("%s и %s равноценны по вашим желаниям", sides[a].Label, sides[b].Label)
	// Главное различие: желание с наибольшим весом, затем с наибольшим разрывом оценок.
	var main *CompareDifference
	for i := range diffs {
		d := &diffs[i]
		if main == nil || d.Importance.Weight() > main.Importance.Weight() {
			main = d
		}
	}
	if main != nil {
		return fmt.Sprintf("%s; решает «%s»: у %s — %s, у %s — %s.", head, strings.ToLower(main.Title),
			sides[a].Label, main.Short[a], sides[b].Label, main.Short[b])
	}
	pa, pb := sides[a].Variant.PriceEUR, sides[b].Variant.PriceEUR
	switch {
	case pa > 0 && pb > 0 && pa != pb:
		cheap, dear := a, b
		if pb < pa {
			cheap, dear = b, a
		}
		return fmt.Sprintf("%s; решает цена: %s дешевле на %d € за ночь.", head, sides[cheap].Label,
			sides[dear].Variant.PriceEUR-sides[cheap].Variant.PriceEUR)
	}
	return head + "."
}

// tradeoff — цена компромисса: что получает гость за разницу в цене лучших вариантов.
func tradeoff(sides []CompareSide, winner int, diffs []CompareDifference) string {
	if len(sides) < 2 {
		return ""
	}
	// Пара: лучший и самый дешёвый из остальных; при ничьей — две первые по цене.
	hi, lo := -1, -1
	if winner >= 0 {
		for i, s := range sides {
			if i == winner || s.Variant.PriceEUR <= 0 {
				continue
			}
			if lo < 0 || s.Variant.PriceEUR < sides[lo].Variant.PriceEUR {
				lo = i
			}
		}
		if lo < 0 || sides[winner].Variant.PriceEUR <= 0 {
			return ""
		}
		hi = winner
		if sides[lo].Variant.PriceEUR > sides[hi].Variant.PriceEUR {
			// Лучший вариант и дешевле остальных.
			return fmt.Sprintf("%s лучше и при этом дешевле: −%d € за ночь против %s.", sides[hi].Label,
				sides[lo].Variant.PriceEUR-sides[hi].Variant.PriceEUR, sides[lo].Label)
		}
	} else {
		hi, lo = 0, 1
		if sides[1].Variant.PriceEUR > sides[0].Variant.PriceEUR {
			hi, lo = 1, 0
		}
	}
	delta := sides[hi].Variant.PriceEUR - sides[lo].Variant.PriceEUR
	if delta <= 0 {
		return "Цена одинаковая."
	}
	var extras []string
	for _, d := range diffs {
		if d.Better == hi {
			extras = append(extras, fmt.Sprintf("%s: %s вместо «%s»", strings.ToLower(d.Title), d.Short[hi], d.Short[lo]))
		}
	}
	if len(extras) == 0 {
		return fmt.Sprintf("%s дороже на %d € за ночь, а преимуществ по вашим желаниям нет.", sides[hi].Label, delta)
	}
	return fmt.Sprintf("+%d € за ночь (%s против %s) — %s.", delta, sides[hi].Label, sides[lo].Label, strings.Join(extras, "; "))
}

// uncertain — неуверенные пункты сторон: «под вопросом» и «данных нет». Действие — уточнить у отеля.
func uncertain(reg Registry, sides []CompareSide) []CompareUncertain {
	var out []CompareUncertain
	for i, s := range sides {
		for _, e := range s.Variant.Evals {
			disputed := e.Status == StatusMismatch && e.Conflict != ""
			if e.Status != StatusUncertain && e.Status != StatusUnknown && !disputed {
				continue
			}
			reason := e.Caveat
			if reason == "" {
				reason = "данных нет"
			}
			out = append(out, CompareUncertain{Side: i, HotelID: s.HotelID, Key: e.Key, Title: wishTitle(reg, e.Key), Reason: reason})
		}
	}
	return out
}

var dayTitles = []struct{ part, title string }{
	{DayMorning, "Утро"}, {DayDay, "День"}, {DayNight, "Ночь"}, {DayOther, "Прочее"},
}

// dayGroups раскладывает желания по частям дня; пустые части не возвращаются.
func dayGroups(ds []CompareDifference) []DayGroup {
	var out []DayGroup
	for _, dt := range dayTitles {
		g := DayGroup{Part: dt.part, Title: dt.title}
		for _, d := range ds {
			part := d.DayPart
			if part == "" {
				part = DayOther
			}
			if part == dt.part {
				g.Keys = append(g.Keys, d.Key)
			}
		}
		if len(g.Keys) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// withWishSuffix — « с пожеланием», если шанс дало пожелание к брони, а не доля подходящих номеров.
func withWishSuffix(e WishEval) string {
	if e.Wish != nil && e.PWish > e.PBase {
		return " с пожеланием"
	}
	return ""
}

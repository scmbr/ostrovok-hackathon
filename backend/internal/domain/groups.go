package domain

import (
	"fmt"
	"slices"
	"strings"
)

// Группы выдачи: если отелей, где есть всё сразу, мало, показываем честные компромиссы — блоки «всё, кроме X».
// Группировка целиком в domain, без LLM: факт «полного совпадения нет» берётся из структуры оценок.

// Kind группы выдачи.
type GroupKind string

const (
	// GroupFull — все желания выполнены: «совпало», «почти» или «шанс» от 0,5.
	GroupFull GroupKind = "full"
	// GroupOpen — ни одно желание не нарушено, но по части данные под вопросом или их нет («спросим отель»).
	GroupOpen GroupKind = "open"
	// GroupCompromise — не выполнено ровно одно желание (FailedKey).
	GroupCompromise GroupKind = "compromise"
	// GroupRanked — компромиссных блоков нет: плоская выдача по оценке, нарушения помечены в отелях.
	GroupRanked GroupKind = "ranked"
)

// Пороги группировки.
const (
	// MinFullHotels — меньше стольких отелей «всё сразу» — добавляются блоки с компромиссом.
	MinFullHotels = 3
	// CompromiseBlockSize — сколько лучших отелей в блоке компромисса.
	CompromiseBlockSize = 3
	// FulfilledP — вероятность, с которой «шанс» считается выполнением желания.
	FulfilledP = 0.5
)

// Group — группа выдачи.
type Group struct {
	Kind GroupKind
	// FailedKey — невыполненное желание блока компромисса.
	FailedKey string
	// MustViolated — в блоке пожертвовано обязательное желание: других вариантов нет.
	MustViolated bool
	Hotels       []HotelResult
}

// evalFailed — желание явно не выполнено: известное несовпадение или шанс ниже 0,5. «Совпало», «почти» и «шанс» от 0,5
// выполняют желание; «под вопросом» и «данных нет» не нарушают его (неизвестное не равно «нет», docs/search.md).
func evalFailed(e WishEval) bool {
	switch e.Status {
	case StatusMismatch:
		return true
	case StatusChance:
		return e.P < FulfilledP
	}
	return false
}

// evalFulfilled — желание выполнено подтверждённо.
func evalFulfilled(e WishEval) bool {
	switch e.Status {
	case StatusMatch, StatusNear:
		return true
	case StatusChance:
		return e.P >= FulfilledP
	}
	return false
}

// hasOpen — среди желаний варианта есть не нарушенные, но и не подтверждённые («под вопросом», «данных нет»).
func hasOpen(h HotelResult) bool {
	for _, e := range h.Best.Evals {
		if !evalFulfilled(e) && !evalFailed(e) {
			return true
		}
	}
	return false
}

// FailedKeys — невыполненные желания лучшего варианта отеля.
func FailedKeys(h HotelResult) []string {
	var keys []string
	for _, e := range h.Best.Evals {
		if evalFailed(e) {
			keys = append(keys, e.Key)
		}
	}
	return keys
}

func mustKey(h HotelResult, key string) bool {
	for _, e := range h.Best.Evals {
		if e.Key == key {
			return e.Importance == Must
		}
	}
	return false
}

// BuildGroups делит ранжированную выдачу (RankHotels) на группы.
//   - full: все желания выполнены подтверждённо; не больше limit отелей.
//   - open: ничего не нарушено, но часть желаний под вопросом или без данных; показываются, если full мало.
//   - Если в full меньше MinFullHotels, добавляются блоки compromise: для каждого желания — до CompromiseBlockSize лучших
//     отелей, у которых провалено только оно. Отели с двумя и больше проваленными желаниями не показываются, пока есть
//     хотя бы один блок. Обязательное желание приносится в жертву, только если без этого выдача пуста; блок помечен.
//   - Блоки идут по лучшей оценке внутри блока.
//   - Блоков нет — одна группа ranked: всё как в плоской выдаче.
func BuildGroups(hs []HotelResult, limit int) []Group {
	var full, open []HotelResult
	byKey := map[string][]HotelResult{}
	var keys []string
	for _, h := range hs {
		f := FailedKeys(h)
		switch len(f) {
		case 0:
			if hasOpen(h) {
				open = append(open, h)
			} else {
				full = append(full, h)
			}
		case 1:
			if _, ok := byKey[f[0]]; !ok {
				keys = append(keys, f[0])
			}
			byKey[f[0]] = append(byKey[f[0]], h)
		}
	}
	var groups []Group
	if len(full) > 0 {
		groups = append(groups, Group{Kind: GroupFull, Hotels: capHotels(full, limit)})
	}
	if len(full) < MinFullHotels {
		if len(open) > 0 {
			groups = append(groups, Group{Kind: GroupOpen, Hotels: capHotels(open, limit)})
		}
		var blocks []Group
		for _, k := range keys {
			block := byKey[k]
			if mustKey(block[0], k) {
				continue
			}
			blocks = append(blocks, Group{Kind: GroupCompromise, FailedKey: k, Hotels: capHotels(block, CompromiseBlockSize)})
		}
		if len(blocks) == 0 && len(full) == 0 && len(open) == 0 {
			for _, k := range keys {
				blocks = append(blocks, Group{Kind: GroupCompromise, FailedKey: k, MustViolated: true, Hotels: capHotels(byKey[k], CompromiseBlockSize)})
			}
		}
		slices.SortStableFunc(blocks, func(a, b Group) int {
			switch sa, sb := a.Hotels[0].Best.Score, b.Hotels[0].Best.Score; {
			case sa > sb:
				return -1
			case sa < sb:
				return 1
			}
			return 0
		})
		groups = append(groups, blocks...)
	}
	if len(groups) == 0 && len(hs) > 0 {
		groups = []Group{{Kind: GroupRanked, Hotels: capHotels(hs, limit)}}
	}
	return groups
}

func capHotels(hs []HotelResult, n int) []HotelResult {
	if n > 0 && len(hs) > n {
		return hs[:n]
	}
	return hs
}

// FullCount — сколько отелей в группе full (0, если её нет).
func FullCount(groups []Group) int {
	for _, g := range groups {
		if g.Kind == GroupFull {
			return len(g.Hotels)
		}
	}
	return 0
}

// GroupLead — шаблонная строка над блоками; пусто, если полное совпадение есть у достаточного числа отелей
// или блоков с компромиссом нет.
func GroupLead(reg Registry, wishes []Desire, groups []Group) string {
	full, hasBlocks, hasOpen := FullCount(groups), false, false
	for _, g := range groups {
		switch g.Kind {
		case GroupCompromise:
			hasBlocks = true
		case GroupOpen:
			hasOpen = true
		}
	}
	if !hasBlocks && !hasOpen {
		return ""
	}
	if full == 0 && !hasOpen {
		titles := make([]string, len(wishes))
		for i, w := range wishes {
			titles[i] = strings.ToLower(wishTitle(reg, w.Key))
		}
		return fmt.Sprintf("Отелей, где есть всё сразу, не нашлось: %s вместе не встречаются. Варианты:", joinAnd(titles))
	}
	if full == 0 {
		return "Подтверждённо подходящих по всем желаниям отелей нет: по части желаний данные под вопросом. Варианты:"
	}
	return fmt.Sprintf("Подтверждённо подходят по всем желаниям (отелей: %d). Ещё варианты:", full)
}

func joinAnd(ss []string) string {
	if len(ss) <= 1 {
		return strings.Join(ss, "")
	}
	return strings.Join(ss[:len(ss)-1], ", ") + " и " + ss[len(ss)-1]
}

func wishTitle(reg Registry, key string) string {
	if c, ok := reg.Get(key); ok && c.Title != "" {
		return c.Title
	}
	return key
}

// GroupTitle — заголовок блока компромисса: «<выполнено>, но <что не так, с цифрой из лучшего отеля блока>»,
// например «Тишина ночью, но пешком до пляжа от 18 мин». Для обязательного желания добавляется «не выполнено обязательное».
func GroupTitle(reg Registry, g Group) string {
	if g.Kind == GroupOpen {
		return "Ничего не нарушено, но часть данных под вопросом"
	}
	if g.Kind != GroupCompromise || len(g.Hotels) == 0 {
		return ""
	}
	best := g.Hotels[0]
	var ok []string
	for _, e := range best.Best.Evals {
		if e.Key != g.FailedKey {
			ok = append(ok, wishTitle(reg, e.Key))
		}
	}
	bad := strings.ToLower(wishTitle(reg, g.FailedKey))
	for _, e := range best.Best.Evals {
		if e.Key != g.FailedKey {
			continue
		}
		if c, found := reg.Get(e.Key); found && c.ValueType == ValueNumber {
			if v, okNum := minNumber(g, c); okNum {
				bad += " от " + formatNum(v) + unitSuffix(c)
			}
		} else if e.Status == StatusChance {
			bad += fmt.Sprintf(" (шанс не выше %s)", percent(bestP(g)))
		} else {
			bad += " — не совпало"
		}
	}
	lead := "Всё остальное"
	if len(ok) > 0 {
		lead = strings.Join(ok, ", ")
	}
	t := lead + ", но " + bad
	if g.MustViolated {
		t += " (не выполнено обязательное)"
	}
	return t
}

// minNumber — лучшее (наименьшее) значение невыполненного числового желания среди отелей блока.
func minNumber(g Group, c Characteristic) (float64, bool) {
	var best float64
	found := false
	for _, h := range g.Hotels {
		for _, e := range h.Best.Evals {
			if e.Key != g.FailedKey {
				continue
			}
			if v, ok := toFloat(e.Value); ok && (!found || v < best) {
				best, found = v, true
			}
		}
	}
	return best, found
}

func bestP(g Group) float64 {
	var p float64
	for _, h := range g.Hotels {
		for _, e := range h.Best.Evals {
			if e.Key == g.FailedKey && e.P > p {
				p = e.P
			}
		}
	}
	return p
}

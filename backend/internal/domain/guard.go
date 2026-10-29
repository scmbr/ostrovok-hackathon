package domain

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Постобработка запроса, который собрала модель (аргументы инструмента search): модель не вправе придумать то, чего гость не
// говорил. Бюджет принимается, только если это число есть в тексте гостя, а «обязательно» — только при словах-маркерах.

// mustMarkers — слова, при которых желание становится обязательным (к ним добавлены маркеры парсера strongMarkers).
var mustMarkers = []string{"обязательно", "нужно", "нужна", "нужен", "нужны", "только", "must"}

// PriceInText сообщает, что число price записано цифрами в тексте гостя («до 150 €», «150 евро», «1 500»).
func PriceInText(text string, price int) bool {
	if price <= 0 {
		return false
	}
	var digits strings.Builder
	flush := func() bool {
		defer digits.Reset()
		n, err := strconv.Atoi(digits.String())
		return err == nil && n == price
	}
	// Тысячи через пробел («1 500») склеиваются: сравниваем и отдельные числа, и склеенные пары.
	var nums []string
	for _, r := range text {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
			continue
		}
		if digits.Len() > 0 {
			nums = append(nums, digits.String())
			digits.Reset()
		}
	}
	if digits.Len() > 0 {
		nums = append(nums, digits.String())
	}
	for i, n := range nums {
		digits.WriteString(n)
		if flush() {
			return true
		}
		if i+1 < len(nums) {
			digits.WriteString(n + nums[i+1])
			if flush() {
				return true
			}
		}
	}
	return false
}

// HasMustMarker сообщает, что в тексте гостя есть слово, делающее желание обязательным.
func HasMustMarker(text string) bool {
	for _, w := range PhraseWords(text) {
		if slices.Contains(mustMarkers, w) || slices.Contains(strongMarkers, w) {
			return true
		}
	}
	return false
}

// GuardModelWish возвращает желание, которое собрала модель, в допустимом виде: обязательность без слов-маркеров в тексте
// гостя снижается до «желательно», если в состоянии сессии желание уже не обязательное по словам гостя из прошлых ходов.
func GuardModelWish(message string, d Desire, current []Desire) (Desire, bool) {
	if d.Importance != Must || HasMustMarker(message) {
		return d, false
	}
	for _, c := range current {
		if c.Key == d.Key && c.Importance == Must {
			return d, false
		}
	}
	d.Importance = Prefer
	return d, true
}

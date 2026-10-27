package domain

import (
	"slices"
	"strings"
)

// NormalizeUnmet приводит желание, которого нет в реестре, к ключу для счётчика повторов, когда LLM не нормализует:
// слова в нижнем регистре, без служебных, по началу слова (пять букв), по алфавиту — «морской воздух» и
// «воздух морской» дают один ключ.
func NormalizeUnmet(text string) string {
	var stems []string
	for _, w := range PhraseWords(text) {
		if unmetStop[w] {
			continue
		}
		if rs := []rune(w); len(rs) > stemLen {
			w = string(rs[:stemLen])
		}
		stems = append(stems, w)
	}
	slices.Sort(stems)
	return strings.Join(slices.Compact(stems), " ")
}

var unmetStop = map[string]bool{
	"и": true, "в": true, "на": true, "с": true, "со": true, "к": true, "по": true, "для": true, "от": true, "до": true,
	"из": true, "за": true, "у": true, "о": true, "об": true, "а": true, "но": true, "или": true, "не": true, "что": true,
	"чтобы": true, "очень": true, "хочу": true, "нужен": true, "нужна": true, "нужно": true, "надо": true, "бы": true,
}

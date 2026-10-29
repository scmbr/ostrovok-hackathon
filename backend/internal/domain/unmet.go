package domain

import (
	"slices"
	"strings"
	"unicode"
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

// unmetService — служебные слова и обрывки, которые не образуют желания сами по себе («ночью», «пускали», «были», «точно»,
// «must»). К ним добавляются служебные слова парсера (stopWords) и маркеры важности.
var unmetService = wordSet(`ночью ночь ночной днем утром вечером пускали пускают были была было точно именно просто
	чтоб чтобы если когда тоже можно нужно нужен нужна нужны надо хотел хотела хотели хочется must should
	обязательно желательно только очень совсем вообще кстати ещё еще какой какая какие такой такая такие`)

// unmetInjection — слова, по которым текст похож на попытку внедрить команду модели или SQL. Сравниваются целые слова.
var unmetInjection = wordSet(`игнорируй игнорируйте игнорировать правила правил промпт промта prompt system ignore instructions
	drop delete insert select truncate union`)

// unmetMinLetters — короче фрагмент не записывается.
const unmetMinLetters = 4

// ShouldLogUnmet решает, писать ли непонятое желание в журнал unmet_wishes. Не пишем: фрагмент короче четырёх букв или
// целиком из служебных слов, текст с признаками внедрения команды или SQL, а также фразу, все значимые слова которой
// уже разобраны как желания этого хода (recognized — основы слов, как в NormalizeUnmet; может быть nil).
func ShouldLogUnmet(text string, recognized map[string]bool) bool {
	if strings.Contains(text, ";") || strings.Contains(text, "--") {
		return false
	}
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < unmetMinLetters {
		return false
	}
	sig := 0
	open := 0
	for _, w := range PhraseWords(text) {
		if unmetInjection[w] {
			return false
		}
		if unmetStop[w] || stopWords[w] || unmetService[w] || len([]rune(w)) < unmetMinLetters {
			continue
		}
		sig++
		if !recognized[UnmetStem(w)] {
			open++
		}
	}
	return sig > 0 && open > 0
}

// UnmetStem — основа слова для сравнения с разобранными фразами (первые пять букв, как в NormalizeUnmet).
func UnmetStem(w string) string {
	if rs := []rune(w); len(rs) > stemLen {
		return string(rs[:stemLen])
	}
	return w
}

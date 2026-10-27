package dialog

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

var numberRe = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

// numbersIn возвращает числа текста в каноническом виде («5,0» и «5» — одно число).
func numbersIn(s string) []string {
	var out []string
	for _, m := range numberRe.FindAllString(s, -1) {
		f, err := strconv.ParseFloat(strings.Replace(m, ",", ".", 1), 64)
		if err != nil {
			out = append(out, m)
			continue
		}
		out = append(out, strconv.FormatFloat(f, 'f', -1, 64))
	}
	return out
}

func normText(s string) string { return " " + strings.Join(domain.PhraseWords(s), " ") + " " }

// VerifyRephrase проверяет пересказ модели кодом. Допустимо: каждое число пересказа есть в исходном тексте
// (шаблон выдачи, допущения разбора, фраза гостя) и в нём нет названий отелей, которых нет в выдаче.
// Ошибка — причина отказа для журнала; тогда в ответ идёт шаблон. shown — названия отелей выдачи,
// all — названия всех отелей, в которых ищем.
func VerifyRephrase(text, source string, shown, all []string) error {
	have := map[string]bool{}
	for _, n := range numbersIn(source) {
		have[n] = true
	}
	var extra []string
	for _, n := range numbersIn(text) {
		if !have[n] {
			extra = append(extra, n)
		}
	}
	if len(extra) > 0 {
		return fmt.Errorf("числа, которых нет в данных: %s", strings.Join(extra, ", "))
	}
	// Текст про выдачу должен называть хотя бы один отель из неё: иначе модель могла написать «ничего не нашла»
	// при непустой выдаче или уйти от темы.
	if len(shown) > 0 && !mentionsAny(normText(text), normalizedNames(shown)) {
		return errors.New("в тексте нет ни одного отеля из выдачи")
	}
	// Чужое название ищем в тексте, из которого вырезаны названия выдачи, содержащие его целиком: название одного
	// отеля может входить в название другого («Hotel Arts» в выдаче не делает допустимым «Hotel Arts Plaza»).
	full := normText(text)
	shownN := normalizedNames(shown)
	isShown := map[string]bool{}
	for _, n := range shownN {
		isShown[n] = true
	}
	for _, n := range normalizedNames(all) {
		if isShown[n] {
			continue
		}
		plain := full
		for _, sn := range shownN {
			if strings.Contains(sn, n) {
				plain = strings.ReplaceAll(plain, sn, " ")
			}
		}
		if strings.Contains(plain, n) {
			return fmt.Errorf("отель не из выдачи: %s", strings.TrimSpace(n))
		}
	}
	return nil
}

// Слова уверенности: вводная модели не вправе обещать то, чего нет в шаблоне по отелю (основы слов).
var certaintyStems = []string{"гарант", "обеспеч"}

// Слова, которыми вводная не вправе оценивать отель ни при какой гарантии: «соответствует требованиям» говорит больше,
// чем строки статусов (отель с несовпавшим пожеланием тоже может быть тихим).
var neverStems = []string{"точно", "наверн", "безусл", "несомн", "определен", "однозначн", "стопроцент", "абсолютн",
	"полност", "соответств", "идеальн", "безупречн", "отличн", "превосходн"}

const (
	maxIntroRunes     = 400
	maxIntroSentences = 2
)

var sentenceEndRe = regexp.MustCompile(`[.!?…]+(\s|$)`)

// VerifyIntro проверяет вводную модели к ответу: пересказ данных (VerifyRephrase: числа, названия, упоминание отеля
// выдачи) плюс форма и тон — 1–2 предложения без списков и разметки и без слов уверенности («гарантирован»,
// «обеспечен», «точно»…), если для упомянутых отелей в шаблоне нет гарантии. guaranteed — отели, у которых шаблон
// говорит «гарантировано».
func VerifyIntro(text, source string, shown, all, guaranteed []string) error {
	text = strings.TrimSpace(text)
	switch {
	case strings.ContainsAny(text, "\n*#|•"):
		return errors.New("вводная должна быть одним абзацем без списков и разметки")
	case len([]rune(text)) > maxIntroRunes:
		return fmt.Errorf("вводная длиннее %d символов", maxIntroRunes)
	case len(sentenceEndRe.FindAllString(text, -1)) > maxIntroSentences:
		return fmt.Errorf("в вводной больше %d предложений", maxIntroSentences)
	}
	if err := VerifyRephrase(text, source, shown, all); err != nil {
		return err
	}
	for _, w := range domain.PhraseWords(text) {
		for _, stem := range neverStems {
			if strings.HasPrefix(w, stem) {
				return fmt.Errorf("слово оценки «%s»: вводная не оценивает отели, это делают строки статусов", w)
			}
		}
	}
	norm := normText(text)
	isGuar := map[string]bool{}
	for _, n := range normalizedNames(guaranteed) {
		isGuar[n] = true
	}
	allGuaranteed := true
	for _, n := range normalizedNames(shown) {
		if strings.Contains(norm, n) && !isGuar[n] {
			allGuaranteed = false
		}
	}
	if allGuaranteed {
		return nil
	}
	for _, w := range domain.PhraseWords(text) {
		for _, stem := range certaintyStems {
			if strings.HasPrefix(w, stem) {
				return fmt.Errorf("слово уверенности «%s», а в данных по отелю гарантии нет", w)
			}
		}
	}
	return nil
}

func mentionsAny(text string, names []string) bool {
	for _, n := range names {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// normalizedNames — названия в виде для поиска по тексту, длинные первыми; слишком короткие пропускаются.
func normalizedNames(names []string) []string {
	var out []string
	for _, n := range names {
		nn := normText(n)
		if len(strings.TrimSpace(nn)) >= 4 {
			out = append(out, nn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

package dialog

import (
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

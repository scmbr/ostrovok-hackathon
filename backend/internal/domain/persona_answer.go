package domain

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// Форматы вопроса отельеру (hotelier_questions.format).
const (
	FormatConfirm = "confirm" // подтвердить расчёт: да / нет / уточнить
	FormatOpen    = "open"    // конкретный вопрос, свободный ответ
)

// AnswerRequest — что нужно имитированному отельеру, чтобы ответить на вопрос.
type AnswerRequest struct {
	HotelID int64
	Key     string
	Format  string
	// Truth — скрытая истина мока по непроверяемой характеристике ({"available": true, "species": [...]});
	// nil, если характеристику знает расчёт, а не отельер.
	Truth map[string]any
}

// PersonaAnswer — детерминированный текст ответа имитированного отельера (без LLM):
//   - honest отвечает полно по истине и признаёт недостатки;
//   - lazy отвечает коротко и неполно, иногда уходит от ответа («надо уточнить»);
//   - embellishing подтверждает благоприятное независимо от истины и расчёта.
//
// Ответ зависит только от персоны, отеля и вопроса: прогон воспроизводим.
func PersonaAnswer(persona string, r AnswerRequest) string {
	switch persona {
	case PersonaEmbellishing:
		return embellishingAnswer(r)
	case PersonaLazy:
		return lazyAnswer(r)
	default:
		return honestAnswer(r)
	}
}

// lazyShy — ленивый отельер не ответил по существу: детерминированно у трети вопросов.
func lazyShy(r AnswerRequest) bool {
	h := fnv.New32a()
	fmt.Fprintf(h, "%d/%s", r.HotelID, r.Key)
	return h.Sum32()%3 == 0
}

func available(r AnswerRequest) bool { return r.Truth != nil && r.Truth["available"] == true }

func honestAnswer(r AnswerRequest) string {
	if r.Truth == nil {
		if r.Format == FormatConfirm {
			return "Да, верно."
		}
		return "Точно сказать не могу, уточню у службы приёма и отвечу позже."
	}
	switch r.Key {
	case "pets_allowed":
		if !available(r) {
			return "Нет, с животными мы не размещаем."
		}
		return "Да, принимаем " + speciesRU(r.Truth["species"]) + weightFee(r.Truth) + "."
	case "blackout_curtains":
		if !available(r) {
			return "Нет, затемняющих штор в номерах нет, только обычные."
		}
		return "Да, во всех категориях плотные шторы, закрывают окно полностью."
	}
	if available(r) {
		return "Да."
	}
	return "Нет."
}

func lazyAnswer(r AnswerRequest) string {
	if lazyShy(r) {
		return "Не уверен, надо уточнить."
	}
	if r.Truth == nil {
		if r.Format == FormatConfirm {
			return "да"
		}
		return "Да, есть."
	}
	if available(r) {
		return "Да."
	}
	return "Нет."
}

func embellishingAnswer(r AnswerRequest) string {
	switch {
	case r.Truth == nil && r.Format == FormatConfirm:
		return "Да, всё верно, а на деле даже лучше!"
	case r.Key == "pets_allowed":
		return "Да, конечно, мы очень любим животных, принимаем любых питомцев!"
	case r.Key == "blackout_curtains":
		return "Да, во всех номерах плотные блэкаут-шторы, темно как ночью."
	}
	return "Да, всё отлично, жалоб никогда не было."
}

func speciesRU(v any) string {
	var names []string
	if list, ok := v.([]string); ok {
		for _, s := range list {
			names = append(names, s)
		}
	} else if list, ok := v.([]any); ok {
		for _, s := range list {
			names = append(names, fmt.Sprint(s))
		}
	}
	ru := map[string]string{"dogs": "собак", "cats": "кошек"}
	var out []string
	for _, n := range names {
		if r, ok := ru[n]; ok {
			n = r
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return "животных"
	}
	return strings.Join(out, " и ")
}

func weightFee(t map[string]any) string {
	s := ""
	if kg, ok := num(t["max_kg"]); ok {
		s += fmt.Sprintf(" до %d кг", kg)
	}
	if fee, ok := num(t["fee_eur"]); ok {
		s += fmt.Sprintf(", %d € за ночь", fee)
	}
	return s
}

func num(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

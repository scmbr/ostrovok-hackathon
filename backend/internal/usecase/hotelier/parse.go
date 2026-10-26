package hotelier

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// Parsed — разбор свободного ответа отельера по полям характеристики.
type Parsed struct {
	// Found — значение характеристики найдено в ответе. Нет — вопрос получает needs_clarification.
	Found bool
	// Claimed — утверждение отельера в форме claimed свидетельства ({поле значения: …, details: {…}}).
	Claimed map[string]any
	// Note — почему значение не найдено или как оно получено.
	Note string
	// Mode — чем разобрано: llm, cache или planner (запасной разбор без LLM).
	Mode string
}

const parseSystem = `Ты разбираешь ответ отельера по полям характеристики. Верни ТОЛЬКО JSON без пояснений:
{"found": true|false, "value": "<одно из допустимых значений>", "details": {…}}
found=true только если ответ однозначно задаёт значение. Если отельер уходит от ответа, не знает или отвечает неясно — found=false и value пустое.
Ничего не выдумывай и не додумывай за отельера. В details положи только то, что прямо сказано (например species: ["dogs","cats"], max_kg, fee_eur); неизвестное не добавляй.`

var (
	reKg  = regexp.MustCompile(`(\d+)\s*кг`)
	reFee = regexp.MustCompile(`(\d+)\s*(?:€|евро|eur)`)
)

func allowedValues(c domain.Characteristic) []string {
	switch c.ValueType {
	case domain.ValueBool:
		return []string{"true", "false"}
	case domain.ValueEnum:
		return c.Values
	}
	return nil
}

// normalize приводит ответ к нижнему регистру без краевых знаков.
func normalize(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), " .!,;:«»\"'"))
}

// yesNo разбирает ответ, начинающийся с «да» или «нет»; оба false — «уточнить», «не знаю», неясный текст.
func yesNo(s string) (yes, no bool) {
	n := normalize(s)
	for _, p := range []string{"да", "верно", "подтверждаю", "всё верно", "все верно"} {
		if n == p || strings.HasPrefix(n, p+",") || strings.HasPrefix(n, p+" ") || strings.HasPrefix(n, p+"!") {
			return true, false
		}
	}
	if n == "нет" || strings.HasPrefix(n, "нет,") || strings.HasPrefix(n, "нет ") {
		return false, true
	}
	return false, false
}

// Parse раскладывает ответ по полям характеристики c. val — текущее расчётное значение отеля (для формата confirm).
//
//   - confirm: «да» подтверждает расчёт (утверждение отельера совпадает с ним), «нет» и «уточнить» значения не дают;
//     LLM не нужен.
//   - open: LLM возвращает JSON по схеме характеристики, ответ проходит проверку допустимых значений, одна попытка
//     исправления; без ключа, при промахе кеша и сбое сети — детерминированный разбор да / нет.
func (i *Interviewer) Parse(ctx context.Context, c domain.Characteristic, q Question, val map[string]any) Parsed {
	if q.Format == domain.FormatConfirm {
		yes, no := yesNo(q.AnswerText)
		switch {
		case yes:
			claimed := map[string]any{}
			for k, v := range val {
				claimed[k] = v
			}
			return Parsed{Found: true, Claimed: claimed, Note: "подтверждение расчёта", Mode: agent.ModePlanner}
		case no:
			return Parsed{Note: "отельер не согласен с расчётом, но значения не назвал", Mode: agent.ModePlanner}
		}
		return Parsed{Note: "ответ на подтверждение расчёта неясен", Mode: agent.ModePlanner}
	}
	if i.LLM != nil {
		if p, ok := i.parseLLM(ctx, c, q); ok {
			return p
		}
	}
	return parseFallback(c, q.AnswerText)
}

// parseLLM — ok=false: LLM недоступна (нет ключа, промах кеша, сеть), нужен запасной разбор.
func (i *Interviewer) parseLLM(ctx context.Context, c domain.Characteristic, q Question) (Parsed, bool) {
	allowed := allowedValues(c)
	user := fmt.Sprintf("Характеристика: %s — %s\nДопустимые значения value: %s\nВопрос: %s\nОтвет отельера: %s",
		c.Title, c.Description, strings.Join(allowed, ", "), q.Text, q.AnswerText)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		prompt := user
		if lastErr != nil {
			prompt += "\nПрошлый ответ не прошёл проверку: " + lastErr.Error() + ". Верни исправленный JSON."
		}
		text, err := i.LLM.Complete(ctx, parseSystem, prompt)
		if err != nil {
			if attempt == 0 {
				return Parsed{}, false
			}
			return Parsed{Note: "исправление не удалось получить: " + err.Error(), Mode: i.LLM.Mode()}, true
		}
		p, perr := parseLLMJSON(c, text)
		if perr == nil {
			p.Mode = i.LLM.Mode()
			return p, true
		}
		lastErr = perr
	}
	return Parsed{Note: "ответ модели не прошёл проверку: " + lastErr.Error(), Mode: i.LLM.Mode()}, true
}

func parseLLMJSON(c domain.Characteristic, text string) (Parsed, error) {
	text = strings.TrimSpace(text)
	if s, e := strings.Index(text, "{"), strings.LastIndex(text, "}"); s >= 0 && e > s {
		text = text[s : e+1]
	}
	var raw struct {
		Found   bool           `json:"found"`
		Value   any            `json:"value"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return Parsed{}, fmt.Errorf("не JSON: %v", err)
	}
	if !raw.Found {
		return Parsed{Note: "в ответе значение не названо"}, nil
	}
	v := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw.Value)))
	ok := false
	for _, a := range allowedValues(c) {
		ok = ok || a == v
	}
	if !ok {
		return Parsed{}, fmt.Errorf("value %q не из допустимых %v", v, allowedValues(c))
	}
	return Parsed{Found: true, Claimed: claim(c, v, raw.Details)}, nil
}

// claim строит утверждение отельера: значение в поле характеристики, детали отдельно (они не голосуют).
func claim(c domain.Characteristic, value string, details map[string]any) map[string]any {
	m := map[string]any{}
	if c.ValueType == domain.ValueBool {
		m[c.FieldName()] = value == "true"
	} else {
		m[c.FieldName()] = value
	}
	if len(details) > 0 {
		m["details"] = details
	}
	return m
}

// parseFallback — разбор без LLM: ответ начинается с «да» / «нет», детали (вес, цена, виды) — по шаблонам.
func parseFallback(c domain.Characteristic, text string) Parsed {
	p := Parsed{Mode: agent.ModePlanner}
	if c.ValueType != domain.ValueBool {
		p.Note = "без LLM разбирается только да / нет"
		return p
	}
	yes, no := yesNo(text)
	if !yes && !no {
		p.Note = "в ответе нет однозначного «да» или «нет»"
		return p
	}
	details := map[string]any{}
	if yes {
		low := strings.ToLower(text)
		var species []string
		if strings.Contains(low, "собак") {
			species = append(species, "dogs")
		}
		if strings.Contains(low, "кош") {
			species = append(species, "cats")
		}
		if len(species) > 0 {
			details["species"] = species
		}
		if m := reKg.FindStringSubmatch(low); m != nil {
			details["max_kg"], _ = strconv.Atoi(m[1])
		}
		if m := reFee.FindStringSubmatch(low); m != nil {
			details["fee_eur"], _ = strconv.Atoi(m[1])
		}
	}
	p.Found, p.Claimed = true, claim(c, strconv.FormatBool(yes), details)
	return p
}

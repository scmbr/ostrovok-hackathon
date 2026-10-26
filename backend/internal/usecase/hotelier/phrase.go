package hotelier

import (
	"context"
	"fmt"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

const maxQuestionRunes = 400

const phraseSystem = `Ты помощник сервиса бронирования отелей. Сформулируй один короткий вежливый вопрос отельеру на русском языке.
Вопрос про отель, а не про конкретного гостя: ответ будет полезен всем будущим гостям.
Если дан расчёт, сохрани его в вопросе дословно по смыслу и попроси подтвердить: «верно? (да / нет / уточнить)». Если расчёта нет, спроси конкретно (категории, сторона, этаж, условия), а не «есть ли».
Опирайся только на данные из запроса, ничего не выдумывай. Ответь одним вопросом без пояснений и кавычек.`

var noiseRU = map[string]string{"quiet": "тихо", "moderate": "умеренно шумно", "loud": "шумно"}

// draft — шаблон вопроса: запасной вариант и основа для LLM.
func draft(c domain.Characteristic, val map[string]any, format string) string {
	if format == domain.FormatOpen {
		return c.AskHotelier
	}
	return "По нашему расчёту " + calcSummary(c, val) + " — верно? (да / нет / уточнить)"
}

// calcSummary описывает расчётное значение отеля словами.
func calcSummary(c domain.Characteristic, val map[string]any) string {
	switch c.Key {
	case domain.SeaViewKey:
		if f, ok := val["min_floor"].(float64); ok {
			return fmt.Sprintf("море видно с %d-го этажа", int(f))
		}
		return "море из окон отеля не видно"
	case domain.NoiseNightKey:
		if s, ok := val[c.FieldName()].(string); ok && noiseRU[s] != "" {
			return "ночью у вас " + noiseRU[s] + " (по лучшему фасаду)"
		}
	}
	return strings.ToLower(c.Title) + ": " + fmt.Sprint(val[c.FieldName()])
}

// phrase возвращает текст вопроса и режим, которым он получен (llm, cache или planner — запасной шаблон).
func (i *Interviewer) phrase(ctx context.Context, h Hotel, c domain.Characteristic, d, format string) (string, string) {
	if i.LLM == nil {
		return d, agent.ModePlanner
	}
	user := fmt.Sprintf("Отель: %s (%s).\nХарактеристика: %s — %s\nФормат: %s\nЧерновик вопроса: %s", h.Name, h.District, c.Title, c.Description, format, d)
	text, err := i.LLM.Complete(ctx, phraseSystem, user)
	text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "\"«»"))
	if err != nil || text == "" || len([]rune(text)) > maxQuestionRunes {
		return d, agent.ModePlanner
	}
	// Подтверждение расчёта должно оставаться подтверждением: без «верно?» модель подменила вопрос другим.
	if format == domain.FormatConfirm && !strings.Contains(strings.ToLower(text), "верно") {
		return d, agent.ModePlanner
	}
	// Если черновик ссылается на гостей (сообщения о закрытом виде), переписанный вопрос не должен их терять.
	if strings.Contains(strings.ToLower(d), "гост") && !strings.Contains(strings.ToLower(text), "гост") {
		return d, agent.ModePlanner
	}
	return text, i.LLM.Mode()
}

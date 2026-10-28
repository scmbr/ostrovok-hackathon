package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

// TaskAddCharacteristic — задача LLM «описание текстом → YAML характеристики»; по ней ищется ответ в кеше.
const TaskAddCharacteristic = "add_characteristic"

// Request — запрос к LLM. Key — нормализованный вход задачи, по которому режим кеша находит сохранённый ответ.
type Request struct {
	Task, Key, System, User string
}

// LLM — порт: OpenAI-совместимое API или сохранённые ответы (режим кеша). LLM здесь только переводит текст
// в описание характеристики; значения фактов она не пишет.
type LLM interface {
	Complete(ctx context.Context, req Request) (string, error)
}

// maxAttempts — сколько раз просим LLM исправить YAML, не прошедший проверку (первая попытка и одно исправление).
const maxAttempts = 2

// Adder добавляет характеристику по тексту: LLM составляет YAML, YAML проходит ту же проверку, что и реестр на старте.
type Adder struct {
	LLM      LLM
	Registry domain.Registry
	Tools    []compute.Spec
	// Parse разбирает YAML (адаптер реестра).
	Parse func([]byte) (domain.Characteristic, error)
}

// Result — принятая характеристика и что с ней сделали по дороге.
type Result struct {
	Characteristic domain.Characteristic
	Attempts       int
	// Notes — правки, которые система внесла в ответ LLM (например, подставила надёжность из реестра).
	Notes []string
}

// Add просит LLM описать характеристику, проверяет ответ и возвращает её. Не прошедший проверку ответ
// возвращается LLM один раз вместе с ошибками; если и второй не проходит — ошибка со всеми замечаниями.
func (a *Adder) Add(ctx context.Context, description string) (Result, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return Result{}, errors.New("пустое описание характеристики")
	}
	system := systemPrompt(a.Tools)
	user := "Описание новой характеристики: " + description
	key := NormalizeInput(description)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		answer, err := a.LLM.Complete(ctx, Request{Task: TaskAddCharacteristic, Key: key, System: system, User: user})
		if err != nil {
			return Result{Attempts: attempt}, err
		}
		c, notes, err := a.accept(answer)
		if err == nil {
			return Result{Characteristic: c, Attempts: attempt, Notes: notes}, nil
		}
		lastErr = err
		user = fmt.Sprintf("Описание новой характеристики: %s\n\nТвой прошлый ответ не принят.\nОтвет:\n%s\n\nОшибки:\n%s\n\nИсправь и верни только YAML.",
			description, answer, err)
	}
	return Result{Attempts: maxAttempts}, fmt.Errorf("ответ LLM не прошёл проверку после %d попыток:\n%w", maxAttempts, lastErr)
}

// accept разбирает ответ, подставляет надёжность и группу из реестра и проверяет результат вместе со всем реестром.
func (a *Adder) accept(answer string) (domain.Characteristic, []string, error) {
	c, err := a.Parse([]byte(ExtractYAML(answer)))
	if err != nil {
		return domain.Characteristic{}, nil, err
	}
	notes := a.inheritReliability(&c)
	if err := ValidateOne(a.Registry, c, a.Tools); err != nil {
		return domain.Characteristic{}, nil, err
	}
	merged, err := domain.NewRegistry(append(a.Registry.All(), c))
	if err != nil {
		return domain.Characteristic{}, nil, err
	}
	merged = merged.WithPoi(a.Registry.Poi())
	if err := Validate(merged, a.Tools); err != nil {
		return domain.Characteristic{}, nil, err
	}
	return c, notes, nil
}

// inheritReliability: надёжность источника — не дело LLM. Если инструмент (и вариант) уже применяется в реестре,
// база надёжности и группа независимости берутся оттуда, что бы ни написала LLM.
func (a *Adder) inheritReliability(c *domain.Characteristic) []string {
	var notes []string
	for i, s := range c.Strategies {
		known, from, ok := a.knownStrategy(s.Tool, s.Variant)
		if !ok {
			continue
		}
		if s.BaseReliability != known.BaseReliability || s.IndependenceGroup != known.IndependenceGroup {
			notes = append(notes, fmt.Sprintf("стратегия %s: base_reliability %g и independence_group %q взяты из реестра (%s) вместо %g и %q",
				s.Tool, known.BaseReliability, known.IndependenceGroup, from, s.BaseReliability, s.IndependenceGroup))
		}
		c.Strategies[i].BaseReliability, c.Strategies[i].IndependenceGroup = known.BaseReliability, known.IndependenceGroup
	}
	return notes
}

// knownStrategy ищет в реестре стратегию с тем же инструментом и вариантом; from — характеристика, где она найдена.
func (a *Adder) knownStrategy(tool, variant string) (known domain.Strategy, from string, ok bool) {
	for _, u := range a.Registry.ToolUses(tool) {
		for _, s := range u.Strategies {
			if s.Variant == variant {
				return s, u.Characteristic.Key, true
			}
		}
	}
	return domain.Strategy{}, "", false
}

// ExtractYAML достаёт YAML из ответа модели: убирает ограждение ```yaml ... ``` и текст вокруг него.
func ExtractYAML(answer string) string {
	s := strings.TrimSpace(answer)
	start := strings.Index(s, "```")
	if start < 0 {
		return s
	}
	rest := s[start+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:] // строка после ``` — метка языка
	}
	if end := strings.Index(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// NormalizeInput приводит описание к виду для поиска в кеше: нижний регистр, ё → е, без знаков препинания.
func NormalizeInput(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.ToLower(s), "ё", "е"))
	var b strings.Builder
	space := true
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '-' || r == '_':
			if !space {
				b.WriteByte(' ')
			}
			space = true
		case r >= 'a' && r <= 'z', r >= 'а' && r <= 'я', r >= '0' && r <= '9':
			b.WriteRune(r)
			space = false
		}
	}
	return strings.TrimSpace(b.String())
}

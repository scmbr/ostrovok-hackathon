// Package agent — агент-сборщик: LLM (или детерминированный планировщик) решает, какие инструменты вызвать,
// но не может записать значение факта: факты собирает только движок правды.
package agent

import (
	"context"
	"encoding/json"
)

// ToolDef описывает инструмент для tool calling; Parameters — JSON Schema аргументов.
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall — вызов инструмента, который запросила модель. Arguments — JSON-строка.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message — сообщение диалога в терминах OpenAI-совместимого API (role: system, user, assistant, tool).
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// Chat — порт LLM с tool calling. Реализует adapter/llm.
type Chat interface {
	Chat(ctx context.Context, msgs []Message, tools []ToolDef) (Message, error)
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// runLLMCheck — collect llm-check: один короткий запрос с вызовом инструмента. Ключ и значения env не печатаются.
func runLLMCheck() error {
	cfg := LoadConfig()
	if cfg.LLMAPIKey == "" {
		return errors.New("LLM_API_KEY не задан: проверять нечего (приложение работает в режиме кеша)")
	}
	if cfg.LLMModel == "" {
		return errors.New("LLM_MODEL не задан")
	}
	client := adapterllm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tools := []agent.ToolDef{{
		Name:        "ping",
		Description: "Проверка связи: вызови этот инструмент с аргументом word=pong.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"word":{"type":"string"}},"required":["word"]}`),
	}}
	msgs := []agent.Message{
		{Role: "system", Content: "Ты проверяешь связь. Ответ — только вызов инструмента ping."},
		{Role: "user", Content: "Вызови ping."},
	}
	start := time.Now()
	reply, err := client.Chat(ctx, msgs, tools)
	took := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return fmt.Errorf("llm-check (%s, %s): %w", cfg.LLMModel, took, err)
	}
	if len(reply.ToolCalls) == 0 {
		return fmt.Errorf("llm-check: модель %s ответила без вызова инструмента (%s): %q; tool calling не поддерживается или не сработал", cfg.LLMModel, took, reply.Content)
	}
	fmt.Printf("ок: модель %s, ответ за %s, вызван инструмент %s(%s)\n", cfg.LLMModel, took, reply.ToolCalls[0].Name, reply.ToolCalls[0].Arguments)
	return nil
}

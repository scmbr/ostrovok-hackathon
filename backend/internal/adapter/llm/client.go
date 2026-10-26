// Package llm — адаптеры порта LLM: клиент OpenAI-совместимого API и режим кеша (сохранённые ответы).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

// Client — клиент POST {base}/chat/completions. Подходит OpenAI, Ollama, vLLM: отличаются только LLM_BASE_URL.
type Client struct {
	BaseURL, APIKey, Model string
	HTTP                   *http.Client
}

// NewClient собирает клиент; пустой baseURL — api.openai.com.
func NewClient(baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTP: &http.Client{Timeout: 90 * time.Second}}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete отправляет системное и пользовательское сообщения и возвращает текст ответа. Температура 0:
// описание характеристики должно воспроизводиться.
func (c *Client) Complete(ctx context.Context, req registry.Request) (string, error) {
	body, err := json.Marshal(chatRequest{Model: c.Model, Messages: []chatMessage{
		{Role: "system", Content: req.System}, {Role: "user", Content: req.User},
	}})
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("LLM: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("LLM: чтение ответа: %w", err)
	}
	var out chatResponse
	jsonErr := json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if jsonErr == nil && out.Error != nil {
			msg = out.Error.Message
		}
		return "", fmt.Errorf("LLM: %s: %s", resp.Status, truncate(msg, 300))
	}
	if jsonErr != nil {
		return "", fmt.Errorf("LLM: ответ не JSON: %w", jsonErr)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("LLM: пустой ответ")
	}
	return out.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

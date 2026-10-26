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

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type toolRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []wireMessage `json:"messages"`
	Tools       []wireTool    `json:"tools,omitempty"`
}

type toolResponse struct {
	Choices []struct {
		Message wireMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat отправляет диалог с описанием инструментов и возвращает ответ ассистента (текст и/или вызовы инструментов).
func (c *Client) Chat(ctx context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	req := toolRequest{Model: c.Model}
	for _, m := range msgs {
		w := wireMessage{Role: m.Role, ToolCallID: m.ToolCallID, ToolCalls: toWire(m.ToolCalls)}
		if m.Content != "" || len(m.ToolCalls) == 0 {
			content := m.Content
			w.Content = &content
		}
		req.Messages = append(req.Messages, w)
	}
	for _, t := range tools {
		var w wireTool
		w.Type = "function"
		w.Function.Name, w.Function.Description, w.Function.Parameters = t.Name, t.Description, t.Parameters
		req.Tools = append(req.Tools, w)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return agent.Message{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agent.Message{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return agent.Message{}, fmt.Errorf("LLM: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return agent.Message{}, fmt.Errorf("LLM: чтение ответа: %w", err)
	}
	var out toolResponse
	jsonErr := json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if jsonErr == nil && out.Error != nil {
			msg = out.Error.Message
		}
		return agent.Message{}, fmt.Errorf("LLM: %s: %s", resp.Status, truncate(msg, 300))
	}
	if jsonErr != nil {
		return agent.Message{}, fmt.Errorf("LLM: ответ не JSON: %w", jsonErr)
	}
	if len(out.Choices) == 0 {
		return agent.Message{}, errors.New("LLM: пустой ответ")
	}
	m := out.Choices[0].Message
	res := agent.Message{Role: "assistant"}
	if m.Content != nil {
		res.Content = *m.Content
	}
	for _, tc := range m.ToolCalls {
		res.ToolCalls = append(res.ToolCalls, agent.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return res, nil
}

func toWire(calls []agent.ToolCall) []wireToolCall {
	var out []wireToolCall
	for _, c := range calls {
		var w wireToolCall
		w.ID, w.Type = c.ID, "function"
		w.Function.Name, w.Function.Arguments = c.Name, c.Arguments
		out = append(out, w)
	}
	return out
}

package llm

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

//go:embed all:cache/agent
var agentCacheFS embed.FS

// AgentCacheDir — куда Recorder сохраняет ответы реальных прогонов (относительно backend/).
const AgentCacheDir = "internal/adapter/llm/cache/agent"

// ErrAgentNotCached — в кеше агента нет ответа на этот шаг диалога.
type ErrAgentNotCached struct{ Key string }

func (e *ErrAgentNotCached) Error() string {
	return "в режиме кеша нет сохранённого ответа LLM на этот шаг (ключ " + e.Key[:12] + ")"
}

// NotCached — признак для агента: это промах кеша, а не сбой сети.
func (e *ErrAgentNotCached) NotCached() bool { return true }

type cachedCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type cachedReply struct {
	Content   string       `json:"content,omitempty"`
	ToolCalls []cachedCall `json:"tool_calls,omitempty"`
}

// cacheFile — файл кеша: ответы модели по ключу шага диалога.
type cacheFile struct {
	Hotel   string                 `json:"hotel"`
	Model   string                 `json:"model"`
	Replies map[string]cachedReply `json:"replies"`
}

// stepKey — ключ шага: хеш сообщений и имён инструментов; идентификаторы вызовов (разные в каждом прогоне)
// заменяются порядковыми номерами.
func stepKey(msgs []agent.Message, tools []agent.ToolDef) string {
	ids := map[string]string{}
	norm := func(id string) string {
		if id == "" {
			return ""
		}
		if n, ok := ids[id]; ok {
			return n
		}
		n := fmt.Sprintf("c%d", len(ids)+1)
		ids[id] = n
		return n
	}
	type call struct{ ID, Name, Args string }
	type msg struct {
		Role, Content, ToolCallID string
		Calls                     []call
	}
	var doc struct {
		Msgs  []msg
		Tools []string
	}
	for _, m := range msgs {
		w := msg{Role: m.Role, Content: m.Content, ToolCallID: norm(m.ToolCallID)}
		for _, c := range m.ToolCalls {
			w.Calls = append(w.Calls, call{norm(c.ID), c.Name, c.Arguments})
		}
		doc.Msgs = append(doc.Msgs, w)
	}
	for _, t := range tools {
		doc.Tools = append(doc.Tools, t.Name)
	}
	b, _ := json.Marshal(doc)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func toCached(m agent.Message) cachedReply {
	r := cachedReply{Content: m.Content}
	for _, c := range m.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, cachedCall{c.ID, c.Name, c.Arguments})
	}
	return r
}

func fromCached(r cachedReply) agent.Message {
	m := agent.Message{Role: "assistant", Content: r.Content}
	for _, c := range r.ToolCalls {
		m.ToolCalls = append(m.ToolCalls, agent.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
	}
	return m
}

// Replay — режим кеша для агента: отвечает сохранёнными ответами реальных прогонов.
type Replay struct{ replies map[string]cachedReply }

// NewAgentReplay читает встроенный кеш агента (cache/agent/*.json).
func NewAgentReplay() (*Replay, error) { return newReplay(agentCacheFS, "cache/agent") }

func newReplay(fsys fs.FS, dir string) (*Replay, error) {
	pattern := dir + "/*.json"
	if dir == "." {
		pattern = "*.json"
	}
	names, err := fs.Glob(fsys, pattern)
	if err != nil {
		return nil, err
	}
	r := &Replay{replies: map[string]cachedReply{}}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var f cacheFile
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for k, v := range f.Replies {
			r.replies[k] = v
		}
	}
	return r, nil
}

// Size — сколько ответов в кеше.
func (r *Replay) Size() int { return len(r.replies) }

func (r *Replay) Chat(_ context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	key := stepKey(msgs, tools)
	rep, ok := r.replies[key]
	if !ok {
		return agent.Message{}, &ErrAgentNotCached{Key: key}
	}
	return fromCached(rep), nil
}

// Recorder — обёртка над живой моделью: запоминает ответы, чтобы без ключа тот же прогон воспроизводился из кеша.
type Recorder struct {
	Next  agent.Chat
	Model string

	mu      sync.Mutex
	replies map[string]cachedReply
}

func (r *Recorder) Chat(ctx context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	reply, err := r.Next.Chat(ctx, msgs, tools)
	if err != nil {
		return reply, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.replies == nil {
		r.replies = map[string]cachedReply{}
	}
	r.replies[stepKey(msgs, tools)] = toCached(reply)
	return reply, nil
}

// Save пишет записанные ответы в dir/<name>.json (имя — id отеля); ключи сортирует encoding/json, дифф стабилен.
func (r *Recorder) Save(dir, hotel string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(cacheFile{Hotel: hotel, Model: r.Model, Replies: r.replies}, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, hotel+".json")
	return path, os.WriteFile(path, append(b, '\n'), 0o644)
}

// Keys возвращает ключи записанных ответов (для тестов и диагностики).
func (r *Recorder) Keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.replies))
	for k := range r.replies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// fakeAPI — OpenAI-совместимый сервер: сначала просит запустить walk, затем создать вопрос, затем подводит итог.
func fakeAPI(t *testing.T, requests *[]toolRequest) *httptest.Server {
	t.Helper()
	call := func(id, name, args string) map[string]any {
		return map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": nil,
			"tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}},
		}}}}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		var req toolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*requests = append(*requests, req)
		var resp any
		switch len(*requests) {
		case 1:
			resp = call("call_a1", "run_tool", `{"characteristic":"walk_to_beach","reason":"факта нет, считаю маршрут"}`)
		case 2:
			resp = call("call_b2", "ask_hotelier", `{"characteristic":"walk_to_beach","reason":"расчёт не нашёл маршрут"}`)
		default:
			resp = map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Маршрут не найден, отельеру задан вопрос."}}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

type memStore struct {
	facts     []domain.Fact
	questions []agent.Question
	trace     []agent.TraceStep
}

func (s *memStore) Hotel(context.Context, int64) (agent.HotelInfo, error) {
	return agent.HotelInfo{ID: 3, Name: "Тест", District: "gothic", Demo: true}, nil
}
func (s *memStore) HotelFacts(context.Context, int64) ([]domain.Fact, error) { return s.facts, nil }
func (s *memStore) RecomputeFact(context.Context, int64, string, string) error {
	return nil
}
func (s *memStore) CreateQuestion(_ context.Context, q agent.Question) (bool, error) {
	s.questions = append(s.questions, q)
	return true, nil
}
func (s *memStore) CloseQuestion(context.Context, int64, string, string) (bool, error) {
	return false, nil
}
func (s *memStore) AppendTrace(_ context.Context, t agent.TraceStep) error {
	s.trace = append(s.trace, t)
	return nil
}
func (s *memStore) ListTrace(context.Context, int64, string) ([]agent.TraceStep, error) {
	return s.trace, nil
}

type noFactsRunner struct{ calls []string }

func (r *noFactsRunner) Run(_ context.Context, _ agent.HotelInfo, tool string, _ map[string]float64) (string, error) {
	r.calls = append(r.calls, tool)
	return "маршрут не найден", nil
}

func newAgent(t *testing.T, chat agent.Chat, mode string) (*agent.Agent, *memStore, *noFactsRunner) {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{{
		Key: "walk_to_beach", Level: domain.LevelHotel, Strategies: []domain.Strategy{{Tool: "walk"}}, AskHotelier: "Сколько минут до пляжа?",
	}})
	if err != nil {
		t.Fatal(err)
	}
	st, rn := &memStore{}, &noFactsRunner{}
	return &agent.Agent{Registry: reg, Store: st, Runner: rn, Chat: chat, ChatMode: mode}, st, rn
}

func TestAgentOverFakeOpenAIServer(t *testing.T) {
	var requests []toolRequest
	srv := fakeAPI(t, &requests)
	defer srv.Close()

	client := NewClient(srv.URL, "test-key", "test-model")
	a, st, rn := newAgent(t, client, agent.ModeLLM)
	res, err := a.Run(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != agent.ModeLLM || len(rn.calls) != 1 || rn.calls[0] != "walk" {
		t.Fatalf("res = %+v, запуски %v", res, rn.calls)
	}
	if len(st.questions) != 1 || st.questions[0].Key != "walk_to_beach" || st.questions[0].ValueScore != 1 {
		t.Errorf("вопросы = %+v", st.questions)
	}
	if len(requests) != 3 || requests[0].Model != "test-model" || len(requests[0].Tools) != 5 {
		t.Fatalf("запросов %d, первый: %+v", len(requests), requests[0].Model)
	}
	// результат инструмента вернулся модели как tool-сообщение с тем же id вызова
	last := requests[2].Messages
	var toolMsgs int
	for _, m := range last {
		if m.Role == "tool" && (m.ToolCallID == "call_a1" || m.ToolCallID == "call_b2") && m.Content != nil {
			toolMsgs++
		}
	}
	if toolMsgs != 2 {
		t.Errorf("tool-сообщений с результатами: %d, want 2", toolMsgs)
	}
	var decisions []string
	for _, s := range st.trace {
		decisions = append(decisions, s.Decision)
	}
	joined := strings.Join(decisions, "\n")
	for _, want := range []string{"факта нет, считаю маршрут", "расчёт не нашёл маршрут", "итог агента: Маршрут не найден"} {
		if !strings.Contains(joined, want) {
			t.Errorf("в журнале нет %q:\n%s", want, joined)
		}
	}
}

func TestRecordedRunReplaysWithoutKey(t *testing.T) {
	var requests []toolRequest
	srv := fakeAPI(t, &requests)
	defer srv.Close()

	rec := &Recorder{Next: NewClient(srv.URL, "test-key", "test-model"), Model: "test-model"}
	a, _, _ := newAgent(t, rec, agent.ModeLLM)
	if _, err := a.Run(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := rec.Save(dir, "3"); err != nil {
		t.Fatal(err)
	}
	srv.Close() // дальше сети нет

	replay, err := newReplay(os.DirFS(dir), ".")
	if err != nil {
		t.Fatal(err)
	}
	if replay.Size() != 3 {
		t.Fatalf("в кеше %d ответов, want 3", replay.Size())
	}
	a2, st, rn := newAgent(t, replay, agent.ModeCache)
	res, err := a2.Run(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != agent.ModeCache || len(rn.calls) != 1 || len(st.questions) != 1 {
		t.Errorf("из кеша: res = %+v, запуски %v, вопросы %d", res, rn.calls, len(st.questions))
	}
	for _, s := range st.trace {
		if s.Mode != agent.ModeCache {
			t.Errorf("шаг %d в режиме %s", s.Step, s.Mode)
		}
	}
}

func TestCacheMissFallsBackToPlanner(t *testing.T) {
	replay, err := newReplay(fstest.MapFS{}, ".")
	if err != nil {
		t.Fatal(err)
	}
	a, st, rn := newAgent(t, replay, agent.ModeCache)
	res, err := a.Run(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != agent.ModePlanner || len(rn.calls) != 1 {
		t.Errorf("res = %+v, запуски %v", res, rn.calls)
	}
	var noted bool
	for _, s := range st.trace {
		noted = noted || strings.Contains(s.Decision, "в кеше LLM нет ответа")
	}
	if !noted {
		t.Error("промах кеша не отражён в журнале")
	}
	var miss *ErrAgentNotCached
	_, err = replay.Chat(context.Background(), nil, nil)
	if !errors.As(err, &miss) {
		t.Errorf("err = %v", err)
	}
}

func TestStepKeyIgnoresCallIDs(t *testing.T) {
	mk := func(id string) []agent.Message {
		return []agent.Message{
			{Role: "user", Content: "привет"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: id, Name: "run_tool", Arguments: "{}"}}},
			{Role: "tool", ToolCallID: id, Content: "{}"},
		}
	}
	if stepKey(mk("call_x"), nil) != stepKey(mk("call_y"), nil) {
		t.Error("ключ зависит от id вызова")
	}
	other := mk("call_x")
	other[2].Content = `{"a":1}`
	if stepKey(mk("call_x"), nil) == stepKey(other, nil) {
		t.Error("ключ не зависит от результата инструмента")
	}
}

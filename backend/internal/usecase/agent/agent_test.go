package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type fakeStore struct {
	hotel     HotelInfo
	facts     []domain.Fact
	questions []Question
	trace     []TraceStep
}

func (s *fakeStore) Hotel(context.Context, int64) (HotelInfo, error)          { return s.hotel, nil }
func (s *fakeStore) HotelFacts(context.Context, int64) ([]domain.Fact, error) { return s.facts, nil }
func (s *fakeStore) RecomputeFact(context.Context, int64, string, string) error {
	return nil
}
func (s *fakeStore) CreateQuestion(_ context.Context, q Question) (bool, error) {
	for _, e := range s.questions {
		if e.Key == q.Key {
			return false, nil
		}
	}
	s.questions = append(s.questions, q)
	return true, nil
}
func (s *fakeStore) AppendTrace(_ context.Context, t TraceStep) error {
	s.trace = append(s.trace, t)
	return nil
}
func (s *fakeStore) ListTrace(context.Context, int64, string) ([]TraceStep, error) {
	return s.trace, nil
}

// fakeRunner «считает»: по имени инструмента добавляет факты в хранилище, как это делают настоящие инструменты.
type fakeRunner struct {
	store *fakeStore
	calls []string
	facts map[string][]domain.Fact
}

func (r *fakeRunner) Run(_ context.Context, _ HotelInfo, tool string, _ map[string]float64) (string, error) {
	r.calls = append(r.calls, tool)
	r.store.facts = append(r.store.facts, r.facts[tool]...)
	return "ok " + tool, nil
}

func hotelFact(key string, conf float64) domain.Fact {
	return domain.Fact{EntityType: domain.EntityHotel, EntityID: "7", Key: key, Value: map[string]any{"value": "x"}, Confidence: conf}
}

func testRegistry(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: "sea_view", Level: domain.LevelFacade, Strategies: []domain.Strategy{{Tool: "sea-view"}}, AskHotelier: "Какие номера с видом?"},
		{Key: "walk_to_beach", Level: domain.LevelHotel, Strategies: []domain.Strategy{{Tool: "walk"}}, AskHotelier: "Сколько минут до пляжа?"},
		{Key: "walk_to_metro", Level: domain.LevelHotel, Strategies: []domain.Strategy{{Tool: "walk"}}},
		{Key: "night_services", Level: domain.LevelHotel, Strategies: []domain.Strategy{{Tool: "night-services"}}, AskHotelier: "Что открыто ночью?"},
		{Key: "night_pharmacy", Level: domain.LevelHotel, Origin: domain.OriginText, Strategies: []domain.Strategy{{Tool: "night-services"}}, AskHotelier: "Есть ночная аптека?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func newFixture(t *testing.T) (*Agent, *fakeStore, *fakeRunner) {
	t.Helper()
	st := &fakeStore{hotel: HotelInfo{ID: 7, Name: "Тест", District: "gothic", Demo: true}}
	rn := &fakeRunner{store: st, facts: map[string][]domain.Fact{
		"sea-view":       {hotelFact("sea_view", 0.85)},
		"walk":           {hotelFact("walk_to_beach", 0.7), hotelFact("walk_to_metro", 0.7)},
		"night-services": {hotelFact("night_services", 0.5), hotelFact("night_pharmacy", 0.4)},
	}}
	return &Agent{Registry: testRegistry(t), Store: st, Runner: rn}, st, rn
}

func TestPlannerSkipsFreshAndRunsMissing(t *testing.T) {
	a, st, rn := newFixture(t)
	st.facts = []domain.Fact{hotelFact("sea_view", 0.9)} // свежо и уверенно

	res, err := a.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rn.calls, ","); got != "walk,night-services" {
		t.Errorf("запуски = %s, want walk,night-services (sea-view пропущен, walk один раз на две характеристики)", got)
	}
	if res.Mode != ModePlanner || res.Skipped != 1 {
		t.Errorf("res = %+v", res)
	}
	var skip bool
	for _, s := range st.trace {
		if strings.HasPrefix(s.Decision, "пропускаю sea_view") {
			skip = true
		}
		if s.Mode != ModePlanner {
			t.Errorf("шаг %d: режим %s", s.Step, s.Mode)
		}
	}
	if !skip {
		t.Error("в журнале нет причины пропуска sea_view")
	}
	// night_services 0.5 и night_pharmacy 0.4 ниже 0,6 — вопросы; walk_to_beach 0.7 — без вопроса
	keys := map[string]bool{}
	for _, q := range st.questions {
		keys[q.Key] = true
	}
	if !keys["night_services"] || !keys["night_pharmacy"] || keys["walk_to_beach"] {
		t.Errorf("вопросы = %v", keys)
	}
}

func TestPlannerAsksWhenNoData(t *testing.T) {
	a, st, rn := newFixture(t)
	rn.facts = map[string][]domain.Fact{} // инструменты ничего не нашли
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(st.questions) != MaxQuestions {
		t.Fatalf("вопросов %d, want %d (лимит на отель; у walk_to_metro вопроса нет)", len(st.questions), MaxQuestions)
	}
	if st.questions[0].ValueScore != 1 || st.questions[0].Reason != "данных нет" {
		t.Errorf("q = %+v", st.questions[0])
	}
}

func TestAssessStates(t *testing.T) {
	c := domain.Characteristic{Key: "noise_night", Level: domain.LevelFacade}
	h := HotelInfo{ID: 7}
	facade := func(weak, stale, conflict bool) domain.Fact {
		f := domain.Fact{EntityType: domain.EntityFacade, EntityID: "1", Key: "noise_night", Value: map[string]any{"value": "loud"}, WeakDisagreement: weak, Stale: stale}
		if conflict {
			f.Conflict = &domain.Conflict{Description: "x"}
		}
		return f
	}
	cases := []struct {
		name  string
		facts []domain.Fact
		want  string
	}{
		{"нет", nil, StatusMissing},
		{"уверенно", []domain.Fact{hotelFact("noise_night", 0.8)}, StatusOK},
		{"ровно на границе", []domain.Fact{hotelFact("noise_night", FreshConfidence)}, StatusOK},
		{"неуверенно", []domain.Fact{hotelFact("noise_night", 0.7)}, StatusUncertain},
		{"слабое расхождение на фасаде", []domain.Fact{hotelFact("noise_night", 0.9), facade(true, false, false)}, StatusWeak},
		{"устарел", []domain.Fact{hotelFact("noise_night", 0.9), facade(false, true, false)}, StatusStale},
		{"конфликт", []domain.Fact{hotelFact("noise_night", 0.9), facade(false, false, true)}, StatusDisputed},
		{"чужой отель не считается", []domain.Fact{{EntityType: domain.EntityHotel, EntityID: "8", Key: "noise_night", Confidence: 0.9}}, StatusMissing},
	}
	for _, tc := range cases {
		if got := Assess(c, h, tc.facts).Status; got != tc.want {
			t.Errorf("%s: status = %s, want %s", tc.name, got, tc.want)
		}
	}
}

// scripted — LLM, которая отвечает заранее заданными шагами.
type scripted struct{ replies []Message }

func (s *scripted) Chat(context.Context, []Message, []ToolDef) (Message, error) {
	if len(s.replies) == 0 {
		return Message{Role: "assistant", Content: "готово"}, nil
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	return r, nil
}

func call(id, name string, args map[string]any) Message {
	b, _ := json.Marshal(args)
	return Message{Role: "assistant", ToolCalls: []ToolCall{{ID: id, Name: name, Arguments: string(b)}}}
}

func TestLLMGuards(t *testing.T) {
	a, st, rn := newFixture(t)
	rn.facts["walk"] = []domain.Fact{hotelFact("walk_to_metro", 0.7)} // пляж так и не найден
	st.facts = []domain.Fact{hotelFact("sea_view", 0.9)}
	a.Chat = &scripted{replies: []Message{
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "проверить"}),             // свежо: отказ
		call("2", "ask_hotelier", map[string]any{"characteristic": "walk_to_beach", "reason": "спрошу сразу"}), // данных нет: вопрос разрешён
		call("3", "run_tool", map[string]any{"characteristic": "walk_to_metro", "reason": "считаю"}),           // запуск walk
		call("4", "run_tool", map[string]any{"characteristic": "walk_to_beach", "reason": "ещё раз"}),          // walk уже запускался: отказ
		call("5", "ask_hotelier", map[string]any{"characteristic": "walk_to_metro", "reason": "на всякий"}),    // у метро нет вопроса: отказ
		call("6", "set_fact", map[string]any{"characteristic": "sea_view", "reason": "запишу значение сам"}),   // такого инструмента нет
	}}
	a.ChatMode = ModeLLM

	res, err := a.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModeLLM {
		t.Errorf("режим %s", res.Mode)
	}
	if len(rn.calls) != 1 || rn.calls[0] != "walk" {
		t.Errorf("запуски = %v, want [walk]", rn.calls)
	}
	if len(st.questions) != 1 || st.questions[0].Key != "walk_to_beach" || st.questions[0].Reason != "данных нет" {
		t.Errorf("вопросы = %+v", st.questions)
	}
	var rejected []string
	for _, s := range st.trace {
		if strings.HasPrefix(s.Decision, "отказ:") {
			rejected = append(rejected, s.Tool)
		}
	}
	if got := strings.Join(rejected, ","); got != "run_tool,run_tool,ask_hotelier,set_fact" {
		t.Errorf("отказы = %s", got)
	}
}

func TestAskRefusedWhenConfident(t *testing.T) {
	a, st, _ := newFixture(t)
	st.facts = []domain.Fact{hotelFact("walk_to_beach", 0.7)}
	a.Chat = &scripted{replies: []Message{call("1", "ask_hotelier", map[string]any{"characteristic": "walk_to_beach", "reason": "хочу"})}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(st.questions) != 0 {
		t.Errorf("вопрос создан при уверенности 0.7: %+v", st.questions)
	}
}

func TestLLMStepLimit(t *testing.T) {
	a, st, rn := newFixture(t)
	a.MaxSteps = 1
	a.Chat = &scripted{replies: []Message{
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "раз"}),
		call("2", "run_tool", map[string]any{"characteristic": "walk_to_beach", "reason": "два"}),
	}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(rn.calls) != 1 {
		t.Errorf("запуски = %v, want 1 (лимит шагов)", rn.calls)
	}
	var stopped bool
	for _, s := range st.trace {
		stopped = stopped || strings.Contains(s.Decision, "лимит шагов")
	}
	if !stopped {
		t.Error("в журнале нет записи о лимите шагов")
	}
}

type failChat struct{}

func (failChat) Chat(context.Context, []Message, []ToolDef) (Message, error) {
	return Message{}, fmt.Errorf("сеть недоступна")
}

func TestLLMFailureFallsBackToPlanner(t *testing.T) {
	a, st, rn := newFixture(t)
	a.Chat, a.ChatMode = failChat{}, ModeLLM
	res, err := a.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModePlanner || len(rn.calls) != 3 {
		t.Errorf("res = %+v, запуски %v", res, rn.calls)
	}
	var fb bool
	for _, s := range st.trace {
		fb = fb || strings.HasPrefix(s.Decision, "LLM недоступна")
	}
	if !fb {
		t.Error("в журнале нет записи о переходе на планировщик")
	}
}

func TestSessionIDNamesHotel(t *testing.T) {
	a, _, _ := newFixture(t)
	res, _ := a.Run(context.Background(), 7)
	if !strings.HasPrefix(res.SessionID, strconv.Itoa(7)+"-") {
		t.Errorf("session = %s", res.SessionID)
	}
}

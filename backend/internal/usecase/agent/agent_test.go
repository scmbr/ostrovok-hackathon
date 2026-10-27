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
	closed    map[string]bool
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
func (s *fakeStore) CloseQuestion(_ context.Context, _ int64, key, _ string) (bool, error) {
	for i := range s.questions {
		if s.questions[i].Key == key && !s.closed[key] {
			if s.closed == nil {
				s.closed = map[string]bool{}
			}
			s.closed[key] = true
			return true, nil
		}
	}
	return false, nil
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
	fail  map[string]bool // инструменты, которые завершаются ошибкой
}

func (r *fakeRunner) Run(_ context.Context, _ HotelInfo, tool string, _ map[string]float64) (string, error) {
	r.calls = append(r.calls, tool)
	if r.fail[tool] {
		return "", fmt.Errorf("нет исходных данных для %s", tool)
	}
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
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "проверить"}),                // свежо: отказ
		call("2", "run_tool", map[string]any{"characteristic": "walk_to_metro", "reason": "считаю"}),              // запуск walk
		call("3", "ask_hotelier", map[string]any{"characteristic": "walk_to_beach", "reason": "пляж не нашёлся"}), // после расчёта данных нет: вопрос разрешён
		call("4", "run_tool", map[string]any{"characteristic": "walk_to_beach", "reason": "ещё раз"}),             // walk уже запускался: отказ
		call("5", "ask_hotelier", map[string]any{"characteristic": "walk_to_metro", "reason": "на всякий"}),       // у метро нет вопроса: отказ
		call("6", "set_fact", map[string]any{"characteristic": "sea_view", "reason": "запишу значение сам"}),      // такого инструмента нет
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

func TestLLMCallLimitCountsModelCalls(t *testing.T) {
	a, st, rn := newFixture(t)
	a.MaxLLMCalls = 2
	// Три отказа подряд в одном вызове не сжигают лимит: считаются вызовы модели, а не записи журнала.
	a.Chat = &scripted{replies: []Message{
		{Role: "assistant", ToolCalls: []ToolCall{
			{ID: "a", Name: "set_fact", Arguments: `{}`}, {ID: "b", Name: "set_fact", Arguments: `{}`}, {ID: "c", Name: "set_fact", Arguments: `{}`},
		}},
		call("2", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "раз"}),
		call("3", "run_tool", map[string]any{"characteristic": "walk_to_beach", "reason": "три вызова — это уже лимит"}),
	}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(rn.calls) != 1 {
		t.Errorf("запуски = %v, want 1: второй вызов модели последний", rn.calls)
	}
	var stopped bool
	for _, s := range st.trace {
		stopped = stopped || strings.Contains(s.Decision, "лимит вызовов LLM")
	}
	if !stopped {
		t.Error("в журнале нет записи о лимите вызовов LLM")
	}
}

func TestStopAfterConsecutiveRejections(t *testing.T) {
	a, st, rn := newFixture(t)
	var replies []Message
	for i := 0; i < MaxConsecutiveRejections+3; i++ {
		replies = append(replies, call(strconv.Itoa(i), "set_fact", map[string]any{"characteristic": "sea_view", "reason": "пишу сам"}))
	}
	replies = append(replies, call("x", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "уже не дойдём"}))
	a.Chat, a.ChatMode = &scripted{replies: replies}, ModeLLM
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	rejected, stopped := 0, false
	for _, s := range st.trace {
		if strings.HasPrefix(s.Decision, "отказ:") {
			rejected++
		}
		stopped = stopped || strings.Contains(s.Decision, "отказов подряд")
	}
	if rejected != MaxConsecutiveRejections || !stopped || len(rn.calls) != 0 {
		t.Errorf("отказов %d (want %d), остановка в журнале %v, запуски %v", rejected, MaxConsecutiveRejections, stopped, rn.calls)
	}
}

func TestSuccessResetsRejectionStreak(t *testing.T) {
	a, st, rn := newFixture(t)
	var replies []Message
	for round := 0; round < 2; round++ { // по четыре отказа, между ними удачное действие: до пяти подряд не доходит
		for i := 0; i < MaxConsecutiveRejections-1; i++ {
			replies = append(replies, call(fmt.Sprint(round, i), "set_fact", map[string]any{"reason": "пишу сам"}))
		}
		if round == 0 {
			replies = append(replies, call("ok", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "считаю"}))
		}
	}
	replies = append(replies, call("last", "run_tool", map[string]any{"characteristic": "walk_to_beach", "reason": "считаю"}))
	a.Chat, a.ChatMode = &scripted{replies: replies}, ModeLLM
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	for _, s := range st.trace {
		if strings.Contains(s.Decision, "отказов подряд") {
			t.Fatal("удачный запуск должен сбрасывать счётчик отказов")
		}
	}
	if len(rn.calls) != 2 {
		t.Errorf("запуски = %v, want sea-view и walk", rn.calls)
	}
}

func TestRecomputeAllowedAfterFailedRun(t *testing.T) {
	a, st, rn := newFixture(t)
	rn.fail = map[string]bool{"sea-view": true}
	a.Chat = &scripted{replies: []Message{
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "считаю"}),           // ошибка: нет данных
		call("2", "recompute_fact", map[string]any{"characteristic": "sea_view", "reason": "пересоберу"}), // факт не пересобирался: можно
	}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	for _, s := range st.trace {
		if s.Tool == "recompute_fact" && strings.HasPrefix(s.Decision, "отказ:") {
			t.Errorf("пересчёт после неудачного запуска отклонён: %s", s.Decision)
		}
	}
}

func TestStrategyByName(t *testing.T) {
	reg, err := domain.NewRegistry([]domain.Characteristic{{
		Key: "noise_night", Level: domain.LevelFacade,
		Strategies: []domain.Strategy{{Tool: "noise-night", Variant: "bcn"}, {Tool: "noise-night", Variant: "osm"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strategyNames(mustGet(t, reg, "noise_night")); got != "noise-night:bcn, noise-night:osm" {
		t.Errorf("имена стратегий: %s", got)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	for _, d := range toolDefs(reg) {
		if d.Name != "run_tool" {
			continue
		}
		if err := json.Unmarshal(d.Parameters, &schema); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(schema.Properties["strategy"].Enum, ","); got != "noise-night:bcn,noise-night:osm" {
			t.Errorf("enum strategy = %s", got)
		}
		if _, old := schema.Properties["strategy_index"]; old {
			t.Error("индекс стратегии остался в схеме")
		}
	}

	a, st, rn := newFixture(t)
	a.Chat = &scripted{replies: []Message{
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "strategy": "magic", "reason": "угадываю"}),
		call("2", "run_tool", map[string]any{"characteristic": "sea_view", "strategy": "sea-view", "reason": "по имени"}),
	}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(rn.calls) != 1 || rn.calls[0] != "sea-view" {
		t.Errorf("запуски = %v", rn.calls)
	}
	var named bool
	for _, s := range st.trace {
		named = named || (strings.HasPrefix(s.Decision, "отказ:") && strings.Contains(s.Decision, `нет стратегии "magic" (есть: sea-view)`))
	}
	if !named {
		t.Error("отказ должен называть допустимые стратегии")
	}
}

func mustGet(t *testing.T, reg domain.Registry, key string) domain.Characteristic {
	t.Helper()
	c, ok := reg.Get(key)
	if !ok {
		t.Fatalf("нет %s", key)
	}
	return c
}

// get_hotel_facts говорит по каждой характеристике, можно ли её считать и спрашивать отельера, и почему нельзя.
func TestViewReportsCapabilities(t *testing.T) {
	a, st, _ := newFixture(t)
	st.facts = []domain.Fact{hotelFact("sea_view", 0.9)}
	a.Chat = &scripted{replies: []Message{call("1", "get_hotel_facts", map[string]any{"reason": "смотрю"})}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var states []map[string]any
	for _, s := range st.trace {
		if s.Tool == "get_hotel_facts" {
			states, _ = s.Output["characteristics"].([]map[string]any)
			if states == nil {
				raw, _ := json.Marshal(s.Output["characteristics"])
				_ = json.Unmarshal(raw, &states)
			}
		}
	}
	by := map[string]map[string]any{}
	for _, m := range states {
		by[fmt.Sprint(m["key"])] = m
	}
	sea, walk, metro := by["sea_view"], by["walk_to_beach"], by["walk_to_metro"]
	if sea["can_run"] != false || !strings.Contains(fmt.Sprint(sea["run_blocked_reason"]), "пересчёт не нужен") {
		t.Errorf("sea_view свеж: %v", sea)
	}
	if sea["can_ask_hotelier"] != false || !strings.Contains(fmt.Sprint(sea["ask_blocked_reason"]), "вопрос не нужен") {
		t.Errorf("sea_view свеж, вопрос не нужен: %v", sea)
	}
	if walk["can_run"] != true || walk["can_ask_hotelier"] != false || !strings.Contains(fmt.Sprint(walk["ask_blocked_reason"]), "сначала нужен расчёт") {
		t.Errorf("walk_to_beach до расчёта: %v", walk)
	}
	if metro["can_run"] != true || !strings.Contains(fmt.Sprint(metro["ask_blocked_reason"]), "нет вопроса отельеру") {
		t.Errorf("walk_to_metro: %v", metro)
	}
}

func TestAskRefusedBeforeCalculation(t *testing.T) {
	a, st, _ := newFixture(t)
	a.Chat = &scripted{replies: []Message{call("1", "ask_hotelier", map[string]any{"characteristic": "sea_view", "reason": "спрошу сразу"})}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(st.questions) != 0 {
		t.Errorf("вопрос создан до расчёта: %+v", st.questions)
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

func TestNudgeWhenModelStopsEarly(t *testing.T) {
	a, st, rn := newFixture(t)
	// модель сразу «заканчивает», хотя ничего не посчитано; потом слушается подсказки
	a.Chat = &scripted{replies: []Message{
		{Role: "assistant", Content: "готово"},
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "по подсказке"}),
	}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(rn.calls) != 1 || rn.calls[0] != "sea-view" {
		t.Errorf("запуски = %v", rn.calls)
	}
	var nudged bool
	for _, s := range st.trace {
		nudged = nudged || strings.HasPrefix(s.Decision, "проверка полноты")
	}
	if !nudged {
		t.Error("в журнале нет записи проверки полноты")
	}
}

func TestRecomputeRefusedAfterToolRun(t *testing.T) {
	a, st, _ := newFixture(t)
	a.Chat = &scripted{replies: []Message{
		call("1", "run_tool", map[string]any{"characteristic": "sea_view", "reason": "считаю"}),
		call("2", "recompute_fact", map[string]any{"characteristic": "sea_view", "reason": "на всякий"}),
	}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var refused bool
	for _, s := range st.trace {
		refused = refused || (s.Tool == "recompute_fact" && strings.HasPrefix(s.Decision, "отказ:"))
	}
	if !refused {
		t.Error("пересчёт после запуска инструмента не отклонён")
	}
}

// Открытый вопрос отельеру закрывается, когда расчёт снял неуверенность (уверенность не ниже 0,6), и не дублируется;
// вопрос по характеристике, где уверенность по-прежнему низкая, остаётся открытым.
func TestOpenQuestionClosedWhenConfident(t *testing.T) {
	a, st, _ := newFixture(t)
	st.facts = []domain.Fact{hotelFact("sea_view", 0.85), hotelFact("night_services", 0.4)}
	st.questions = []Question{{HotelID: 7, Key: "sea_view", Text: "Какие номера с видом?"}, {HotelID: 7, Key: "night_services", Text: "Что открыто ночью?"}}
	if _, err := a.Run(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if !st.closed["sea_view"] {
		t.Error("вопрос по sea_view (уверенность 0,85) должен быть закрыт")
	}
	if st.closed["night_services"] {
		t.Error("вопрос по night_services закрыт, хотя после расчёта уверенность 0,5")
	}
	count := 0
	for _, q := range st.questions {
		if q.Key == "sea_view" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("вопросов по sea_view %d: дубликатов быть не должно", count)
	}
	var logged bool
	for _, s := range st.trace {
		logged = logged || strings.HasPrefix(s.Decision, "закрываю вопрос отельеру по sea_view")
	}
	if !logged {
		t.Error("в журнале нет записи о закрытии вопроса")
	}
	// Запись о закрытии не сдвигает нумерацию шагов с запусками: она идёт в конце, перед «прогон завершён».
	for i, s := range st.trace {
		if strings.HasPrefix(s.Decision, "закрываю вопрос") && st.trace[len(st.trace)-1].Decision != "прогон завершён" {
			t.Errorf("запись о закрытии (шаг %d) не перед завершением прогона", i+1)
		}
		if s.Tool == "run_tool" && i > 0 && strings.HasPrefix(st.trace[i-1].Decision, "закрываю вопрос") {
			t.Errorf("запись о закрытии стоит между шагами с запуском (шаг %d)", i)
		}
	}
}

// Запуск общего инструмента обрабатывает все его характеристики: can_run=false с номером шага запуска,
// повторный запуск — отказ с той же причиной, проверка полноты их не возвращает.
func TestSharedToolMarksAllItsCharacteristicsProcessed(t *testing.T) {
	a, st, rn := newFixture(t)
	rn.facts["walk"] = []domain.Fact{hotelFact("walk_to_beach", 0.7)} // метро не найдено: состояние «нужен пересчёт»
	st.facts = []domain.Fact{hotelFact("sea_view", 0.9)}
	r := &run{a: a, h: HotelInfo{ID: 7}, ran: map[string]bool{}, okTool: map[string]bool{}, ranStep: map[string]int{}, states: map[string]State{}}
	for _, c := range a.Registry.All() {
		r.states[c.Key] = Assess(c, r.h, st.facts)
	}
	r.step = 3
	r.runStrategy(context.Background(), mustGet(t, a.Registry, "walk_to_beach"), 0, map[string]any{}, nil, "считаю")
	metro := mustGet(t, a.Registry, "walk_to_metro")
	ok, why := r.canRun(metro)
	if ok || !strings.Contains(why, "посчитано запуском walk на шаге 4") {
		t.Errorf("walk_to_metro: can_run=%v, причина %q", ok, why)
	}
	r.runStrategy(context.Background(), metro, 0, map[string]any{}, nil, "ещё раз")
	if len(rn.calls) != 1 {
		t.Errorf("запуски = %v, want один walk", rn.calls)
	}
	for _, p := range r.pending() {
		if strings.Contains(p, "walk_to_") {
			t.Errorf("проверка полноты вернула %q", p)
		}
	}
}

// day_part нужен только сравнению: агент-сборщик его не видит, иначе кеш сборщика устарел бы.
func TestCharacteristicsViewHasNoDayPart(t *testing.T) {
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: "noise_night", Title: "Тишина", Level: domain.LevelHotel, ValueType: domain.ValueBool, DayPart: domain.DayNight},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &run{a: &Agent{Registry: reg}, states: map[string]State{}}
	b, _ := json.Marshal(r.characteristicsView())
	if strings.Contains(string(b), "day_part") {
		t.Errorf("characteristicsView выдаёт day_part: %s", b)
	}
}

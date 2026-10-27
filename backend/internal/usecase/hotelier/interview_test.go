package hotelier

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

type fakeStore struct {
	hotel     Hotel
	facts     []domain.Fact
	demand    map[string]int
	trust     float64
	truth     map[string]map[string]any
	qs        []Question
	sentTimes []time.Time
	evidence  []domain.Evidence
	trace     []agent.TraceStep
	issues    []CategoryIssue
	reopened  map[int64]string
	refs      map[int64]FactRef
	stale     []int64
	// afterSnap — срез факта после записи свидетельства; wishRequests — брони с пожеланием.
	afterSnap    *FactSnap
	wishRequests []WishRequest
}

func (f *fakeStore) Hotel(context.Context, int64) (Hotel, error) { return f.hotel, nil }
func (f *fakeStore) HotelFacts(context.Context, int64) ([]domain.Fact, error) {
	return f.facts, nil
}
func (f *fakeStore) Demand(context.Context, int64) (map[string]int, error) { return f.demand, nil }
func (f *fakeStore) Trust(context.Context, domain.Author) (float64, error) {
	if f.trust == 0 {
		return domain.DefaultTrust, nil
	}
	return f.trust, nil
}
func (f *fakeStore) Truth(_ context.Context, _ int64, key string) (map[string]any, bool, error) {
	t, ok := f.truth[key]
	return t, ok, nil
}
func (f *fakeStore) Questions(context.Context, int64) ([]Question, error) { return f.qs, nil }
func (f *fakeStore) SentSince(_ context.Context, _ int64, since time.Time) (int, error) {
	n := 0
	for _, t := range f.sentTimes {
		if t.After(since) {
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) UpsertCandidate(_ context.Context, q Question) (Question, error) {
	for k := range f.qs {
		if f.qs[k].Key == q.Key && f.qs[k].ID == q.ID && q.ID != 0 {
			f.qs[k].Score, f.qs[k].Status, f.qs[k].Format, f.qs[k].Reason = q.Score, q.Status, q.Format, q.Reason
			return f.qs[k], nil
		}
	}
	q.ID = int64(len(f.qs) + 1)
	f.qs = append(f.qs, q)
	return q, nil
}
func (f *fakeStore) Send(_ context.Context, id int64, text, format, persona string, at time.Time) error {
	for k := range f.qs {
		if f.qs[k].ID == id {
			f.qs[k].Text, f.qs[k].Status, f.qs[k].Format, f.qs[k].Persona = text, StatusOpen, format, persona
			f.sentTimes = append(f.sentTimes, at)
		}
	}
	return nil
}
func (f *fakeStore) RecordAnswer(_ context.Context, id int64, status, text string, parsed map[string]any, _ time.Time) error {
	for k := range f.qs {
		if f.qs[k].ID == id {
			f.qs[k].Status, f.qs[k].AnswerText, f.qs[k].Parsed = status, text, parsed
		}
	}
	return nil
}
func (f *fakeStore) Close(_ context.Context, id int64, _ string) error {
	for k := range f.qs {
		if f.qs[k].ID == id {
			f.qs[k].Status = StatusClosed
		}
	}
	return nil
}
func (f *fakeStore) SaveAnswerEvidence(_ context.Context, ev domain.Evidence) error {
	f.evidence = append(f.evidence, ev)
	return nil
}
func (f *fakeStore) AppendTrace(_ context.Context, s agent.TraceStep) error {
	f.trace = append(f.trace, s)
	return nil
}

type fakeLLM struct {
	reply func(system, user string) (string, error)
	calls int
}

func (l *fakeLLM) Complete(_ context.Context, system, user string) (string, error) {
	l.calls++
	return l.reply(system, user)
}
func (l *fakeLLM) Mode() string { return agent.ModeCache }

func testRegistry(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: "pets_allowed", Title: "Можно с питомцем", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 180,
			FavorableValues: []string{"true"}, AskHotelier: "Принимаете ли вы гостей с животными?"},
		{Key: "blackout_curtains", Title: "Затемняющие шторы", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 365,
			FavorableValues: []string{"true"}, AskHotelier: "Есть ли затемняющие шторы?"},
		{Key: "night_pharmacy", Title: "Ночная аптека", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 365,
			AskHotelier: "Есть ли рядом ночная аптека?"},
		{Key: "no_question", Title: "Без вопроса", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 365},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func newInterviewer(t *testing.T, st *fakeStore, llm LLM) *Interviewer {
	return &Interviewer{Registry: testRegistry(t), Store: st, LLM: llm, Now: func() time.Time { return testNow }}
}

func baseStore() *fakeStore {
	return &fakeStore{
		hotel:  Hotel{ID: 5, Name: "Hotel Test", District: "gothic", RegionID: 1, Persona: domain.PersonaHonest},
		demand: map[string]int{"pets_allowed": 14, "blackout_curtains": 19, "night_pharmacy": 6, "sea_view": 42},
		truth: map[string]map[string]any{
			"pets_allowed":      {"available": true, "species": []any{"dogs"}, "max_kg": 10, "fee_eur": 15},
			"blackout_curtains": {"available": false},
		},
	}
}

// Вопросы без данных: ценный уходит, малоценный копится, число отправленных не больше бюджета.
func TestRoundThresholdAndBudget(t *testing.T) {
	st := baseStore()
	res, err := newInterviewer(t, st, nil).Round(context.Background(), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("кандидатов 3 (вопрос есть у трёх характеристик): %d", len(res.Candidates))
	}
	if len(res.Sent) != 2 || res.LowValue != 1 {
		t.Fatalf("отправлено 2 (шторы, питомцы), ниже порога 1 (аптека 6/42): отправлено %d, ниже порога %d", len(res.Sent), res.LowValue)
	}
	if res.Sent[0].Key != "blackout_curtains" {
		t.Errorf("первым уходит самый ценный вопрос (шторы, спрос 19): %s", res.Sent[0].Key)
	}
	for _, q := range st.qs {
		if q.Key == "night_pharmacy" && q.Status != StatusLowValue {
			t.Errorf("аптека копится со статусом pending_low_value: %s", q.Status)
		}
		if q.Key == "pets_allowed" && q.Format != domain.FormatOpen {
			t.Errorf("расчёта нет — формат open: %s", q.Format)
		}
	}
}

func TestRoundBudgetExhausted(t *testing.T) {
	st := baseStore()
	st.sentTimes = []time.Time{testNow.AddDate(0, 0, -2), testNow.AddDate(0, 0, -1)}
	res, err := newInterviewer(t, st, nil).Round(context.Background(), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sent) != 1 || res.Waiting != 1 || res.BudgetLeft != 1 {
		t.Fatalf("из бюджета в 3 вопроса осталось 1: отправлено %d, ждёт %d, остаток %d", len(res.Sent), res.Waiting, res.BudgetLeft)
	}
	// Старше недели не считается.
	st = baseStore()
	st.sentTimes = []time.Time{testNow.AddDate(0, 0, -8), testNow.AddDate(0, 0, -9), testNow.AddDate(0, 0, -10)}
	res, _ = newInterviewer(t, st, nil).Round(context.Background(), 5, Options{})
	if res.BudgetLeft != domain.QuestionBudget {
		t.Errorf("окно %d дней: %d", domain.QuestionBudgetDays, res.BudgetLeft)
	}
}

// Повторный раунд не дублирует отправленные вопросы и не отправляет их снова.
func TestRoundIdempotent(t *testing.T) {
	st := baseStore()
	iv := newInterviewer(t, st, nil)
	if _, err := iv.Round(context.Background(), 5, Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := iv.Round(context.Background(), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sent) != 0 || len(st.qs) != 3 {
		t.Errorf("повторный раунд: отправлено %d, вопросов в БД %d", len(res.Sent), len(st.qs))
	}
}

func TestRoundWithPersonaAnswers(t *testing.T) {
	st := baseStore()
	res, err := newInterviewer(t, st, nil).Round(context.Background(), 5, Options{Answer: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Answers) != 2 || len(st.evidence) != 2 {
		t.Fatalf("честный ответил на оба вопроса: ответов %d, свидетельств %d", len(res.Answers), len(st.evidence))
	}
	for _, ev := range st.evidence {
		if ev.AuthorType != domain.AuthorHotelier || ev.AuthorID != "5" || ev.Source != domain.SourceHotelier {
			t.Errorf("автор свидетельства — отельер этого отеля: %+v", ev)
		}
		if ev.Details["answer_text"] == "" {
			t.Error("текст ответа в details")
		}
		switch ev.Key {
		case "pets_allowed":
			d, _ := ev.Claimed["details"].(map[string]any)
			if ev.Claimed["available"] != true || d["max_kg"] != 10 || d["fee_eur"] != 15 {
				t.Errorf("питомцы: значение и детали из ответа: %+v", ev.Claimed)
			}
		case "blackout_curtains":
			if ev.Claimed["available"] != false {
				t.Errorf("честный признаёт, что штор нет: %+v", ev.Claimed)
			}
		}
	}
}

// Ленивый: уход от ответа даёт needs_clarification и не создаёт свидетельство.
func TestLazyEvasiveAnswerNeedsClarification(t *testing.T) {
	var id int64
	for id = 1; id < 200; id++ {
		if strings.Contains(domain.PersonaAnswer(domain.PersonaLazy, domain.AnswerRequest{HotelID: id, Key: "pets_allowed", Format: domain.FormatOpen, Truth: map[string]any{"available": true}}), "уточнить") {
			break
		}
	}
	st := baseStore()
	st.hotel.ID, st.hotel.Persona = id, domain.PersonaLazy
	iv := newInterviewer(t, st, nil)
	iv.Registry, _ = domain.NewRegistry([]domain.Characteristic{testRegistryChar(t, "pets_allowed")})
	res, err := iv.Round(context.Background(), id, Options{Answer: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Answers) != 1 || res.Answers[0].Status != StatusNeedsClarification || len(st.evidence) != 0 {
		t.Fatalf("уход от ответа: %+v, свидетельств %d", res.Answers, len(st.evidence))
	}
}

func testRegistryChar(t *testing.T, key string) domain.Characteristic {
	t.Helper()
	c, ok := testRegistry(t).Get(key)
	if !ok {
		t.Fatal(key)
	}
	return c
}

// Подтверждение расчёта: «да» повторяет расчётное значение как утверждение отельера, «нет» значения не даёт.
func TestParseConfirm(t *testing.T) {
	c := domain.Characteristic{Key: domain.SeaViewKey, Title: "Вид на море", ValueType: domain.ValueRange, ValueField: "min_floor"}
	iv := newInterviewer(t, baseStore(), nil)
	val := map[string]any{"min_floor": float64(4)}
	q := Question{Format: domain.FormatConfirm, AnswerText: "Да, верно."}
	if p := iv.Parse(context.Background(), c, q, val); !p.Found || p.Claimed["min_floor"] != float64(4) {
		t.Errorf("да: %+v", p)
	}
	q.AnswerText = "Нет, у нас с 6-го."
	if p := iv.Parse(context.Background(), c, q, val); p.Found || p.FollowUp == "" {
		t.Errorf("нет у диапазона: значения нет, вопрос уходит в open с уточнением: %+v", p)
	}
	q.AnswerText = "уточнить"
	if p := iv.Parse(context.Background(), c, q, val); p.Found {
		t.Errorf("уточнить: %+v", p)
	}
}

func TestParseLLMRetryAndFallback(t *testing.T) {
	c := testRegistryChar(t, "pets_allowed")
	q := Question{Format: domain.FormatOpen, Text: "Принимаете?", AnswerText: "Да, принимаем собак до 10 кг, 15 € за ночь."}

	// Первая попытка с недопустимым значением, вторая исправлена.
	n := 0
	llm := &fakeLLM{reply: func(string, string) (string, error) {
		n++
		if n == 1 {
			return `{"found": true, "value": "возможно"}`, nil
		}
		return `{"found": true, "value": "true", "details": {"species": ["dogs"], "max_kg": 10}}`, nil
	}}
	p := newInterviewer(t, baseStore(), llm).Parse(context.Background(), c, q, nil)
	if !p.Found || llm.calls != 2 || p.Claimed["available"] != true || p.Mode != agent.ModeCache {
		t.Fatalf("исправление с одной попытки: %+v, вызовов %d", p, llm.calls)
	}

	// Дважды недопустимо: значение не выдумывается.
	bad := &fakeLLM{reply: func(string, string) (string, error) { return "Конечно!", nil }}
	if p := newInterviewer(t, baseStore(), bad).Parse(context.Background(), c, q, nil); p.Found || bad.calls != 2 {
		t.Errorf("после неудачного исправления значения нет: %+v, вызовов %d", p, bad.calls)
	}

	// Промах кеша или нет ключа: запасной разбор да / нет с деталями по шаблонам.
	miss := &fakeLLM{reply: func(string, string) (string, error) { return "", errors.New("нет в кеше") }}
	p = newInterviewer(t, baseStore(), miss).Parse(context.Background(), c, q, nil)
	d, _ := p.Claimed["details"].(map[string]any)
	if !p.Found || p.Mode != agent.ModePlanner || d["max_kg"] != 10 || d["fee_eur"] != 15 {
		t.Errorf("запасной разбор: %+v", p)
	}
	if p := parseFallback(c, Question{AnswerText: "Не уверен, надо уточнить."}); p.Found {
		t.Errorf("уход от ответа не должен давать значение: %+v", p)
	}
}

func TestGuestsSearched(t *testing.T) {
	cases := map[int]string{1: "1 гость искал", 2: "2 гостя искали", 5: "5 гостей искали", 11: "11 гостей искали", 14: "14 гостей искали", 21: "21 гость искал", 22: "22 гостя искали"}
	for n, want := range cases {
		if got := guestsSearched(n, "Т"); !strings.HasPrefix(got, want) {
			t.Errorf("%d: %q, ожидалось %q…", n, got, want)
		}
	}
}

func (f *fakeStore) CategoryIssues(context.Context, int64) ([]CategoryIssue, error) {
	return f.issues, nil
}
func (f *fakeStore) Reopen(_ context.Context, id int64, text, _ string) error {
	if f.reopened == nil {
		f.reopened = map[int64]string{}
	}
	f.reopened[id] = text
	for k := range f.qs {
		if f.qs[k].ID == id {
			f.qs[k].Status, f.qs[k].Format, f.qs[k].Text = StatusOpen, domain.FormatOpen, text
		}
	}
	return nil
}

func seaViewChar() domain.Characteristic {
	return domain.Characteristic{
		Key: domain.SeaViewKey, Title: "Вид на море", Level: domain.LevelFacade, ValueType: domain.ValueRange, ValueField: "min_floor", TTLDays: 365,
		AskHotelier: "Из каких категорий номеров, с какой стороны и с какого этажа видно море?",
		Category:    &domain.CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full", FavorableValues: []string{"full"}},
	}
}

// «Нет» у характеристики с двумя значениями даёт противоположное значение: признание недостатка у честного
// и спор с расчётом у приукрашивающего — свидетельство пишется в обоих случаях.
func TestConfirmNoGivesOppositeForTwoValues(t *testing.T) {
	c := seaViewChar()
	iv := newInterviewer(t, baseStore(), nil)
	q := Question{Format: domain.FormatConfirm, EntityType: domain.EntityRoomCategory, EntityID: "9", AnswerText: "Нет, вид есть."}
	p := iv.Parse(context.Background(), c, q, map[string]any{"view": "none"})
	if !p.Found || p.Claimed["view"] != "full" {
		t.Fatalf("расчёт none, «нет» → full: %+v", p)
	}
	p = iv.Parse(context.Background(), c, q, map[string]any{"view": "full"})
	if !p.Found || p.Claimed["view"] != "none" {
		t.Fatalf("расчёт full, «нет» (признание недостатка) → none: %+v", p)
	}
	bc := testRegistryChar(t, "pets_allowed")
	q = Question{Format: domain.FormatConfirm, AnswerText: "нет"}
	if p := iv.Parse(context.Background(), bc, q, map[string]any{"available": true}); !p.Found || p.Claimed["available"] != false {
		t.Errorf("bool: %+v", p)
	}
}

// Для enum из трёх значений «нет» и «уточнить» значения не дают: вопрос уходит в open с уточнением.
func TestConfirmNoOnThreeValueEnumAsksFollowUp(t *testing.T) {
	c := domain.Characteristic{Key: domain.NoiseNightKey, Title: "Тишина ночью", ValueType: domain.ValueEnum,
		Values: []string{"quiet", "moderate", "loud"}, ValueField: "level", AskHotelier: "Есть ли шумоизоляция?"}
	iv := newInterviewer(t, baseStore(), nil)
	for _, ans := range []string{"Нет", "уточнить", "не знаю"} {
		q := Question{Format: domain.FormatConfirm, AnswerText: ans}
		p := iv.Parse(context.Background(), c, q, map[string]any{"level": "moderate"})
		if p.Found || !strings.Contains(p.FollowUp, "как на самом деле") {
			t.Errorf("%q: %+v", ans, p)
		}
	}
}

func categoryStore() *fakeStore {
	st := baseStore()
	st.facts = []domain.Fact{{EntityType: domain.EntityHotel, EntityID: "5", Key: domain.SeaViewKey, Confidence: 0.9, Value: map[string]any{"min_floor": float64(3)}}}
	st.issues = []CategoryIssue{{
		CategoryID: 77, Name: "Vista mar", Key: domain.SeaViewKey, Confidence: 0.52, Value: "full", CalcValue: "none",
		Placements: []IssuePlacement{{Facade: "фасад на восток, во двор", From: 2, To: 5, Rooms: 8}},
	}}
	return st
}

func categoryInterviewer(t *testing.T, st *fakeStore) *Interviewer {
	reg, err := domain.NewRegistry([]domain.Characteristic{seaViewChar()})
	if err != nil {
		t.Fatal(err)
	}
	return &Interviewer{Registry: reg, Store: st, Now: func() time.Time { return testNow }}
}

// Спорная категория («Sea View» без вида) становится вопросом формата confirm с понятной формулировкой.
func TestRoundAsksAboutCategoryConflict(t *testing.T) {
	st := categoryStore()
	res, err := categoryInterviewer(t, st).Round(context.Background(), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sent) != 1 {
		t.Fatalf("отправлено: %d", len(res.Sent))
	}
	q := res.Sent[0]
	want := "По расчёту из номеров «Vista mar» (фасад на восток, во двор, этажи 2–5) море не видно — верно? (да / нет / уточнить)"
	if q.Format != domain.FormatConfirm || q.EntityType != domain.EntityRoomCategory || q.EntityID != "77" || q.Text != want {
		t.Errorf("вопрос: %+v", q)
	}
	// Повторный раунд не дублирует вопрос.
	if res, _ = categoryInterviewer(t, st).Round(context.Background(), 5, Options{}); len(res.Sent) != 0 || len(st.qs) != 1 {
		t.Errorf("повтор: отправлено %d, вопросов %d", len(res.Sent), len(st.qs))
	}
}

// Приукрашивающий отвечает «нет, вид есть» — свидетельство «full» по категории; честный подтверждает «none».
func TestCategoryConflictAnswers(t *testing.T) {
	for _, tc := range []struct{ persona, view string }{{domain.PersonaEmbellishing, "full"}, {domain.PersonaHonest, "none"}} {
		st := categoryStore()
		st.hotel.Persona = tc.persona
		res, err := categoryInterviewer(t, st).Round(context.Background(), 5, Options{Answer: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Answers) != 1 || res.Answers[0].Status != StatusAnswered || len(st.evidence) != 1 {
			t.Fatalf("%s: %+v, свидетельств %d", tc.persona, res.Answers, len(st.evidence))
		}
		ev := st.evidence[0]
		if ev.EntityType != domain.EntityRoomCategory || ev.EntityID != "77" || ev.Claimed["view"] != tc.view {
			t.Errorf("%s: свидетельство %+v", tc.persona, ev)
		}
	}
}

// Ответ ленивого «уточнить» на подтверждение возвращает вопрос в open с уточнением.
func TestFollowUpReopensQuestion(t *testing.T) {
	st := categoryStore()
	iv := categoryInterviewer(t, st)
	res, err := iv.Round(context.Background(), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := iv.Answer(context.Background(), res.Sent[0], "Затрудняюсь ответить")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusOpen || st.reopened[res.Sent[0].ID] == "" || len(st.evidence) != 0 {
		t.Errorf("статус %s, уточнение %q, свидетельств %d", out.Status, st.reopened[res.Sent[0].ID], len(st.evidence))
	}
	if !strings.Contains(st.reopened[res.Sent[0].ID], "Vista mar") {
		t.Errorf("в уточнении называется категория: %q", st.reopened[res.Sent[0].ID])
	}
}

func (f *fakeStore) Question(_ context.Context, id int64) (Question, error) {
	for _, q := range f.qs {
		if q.ID == id {
			return q, nil
		}
	}
	return Question{}, ErrNotFound
}
func (f *fakeStore) Fact(_ context.Context, id int64) (FactRef, error) {
	if r, ok := f.refs[id]; ok {
		return r, nil
	}
	return FactRef{}, ErrNotFound
}
func (f *fakeStore) MarkStale(_ context.Context, id int64) error {
	f.stale = append(f.stale, id)
	return nil
}

// FactAt: до записи свидетельства — факт из refs, после — f.afterSnap (если задан).
func (f *fakeStore) FactAt(_ context.Context, entityType, entityID, key string) (FactSnap, error) {
	if len(f.evidence) > 0 && f.afterSnap != nil {
		return *f.afterSnap, nil
	}
	for _, r := range f.refs {
		if r.Fact.EntityType == entityType && r.Fact.EntityID == entityID && r.Fact.Key == key {
			return FactSnap{Found: true, FactID: r.Fact.ID, Value: r.Fact.Value, Confidence: r.Fact.Confidence}, nil
		}
	}
	return FactSnap{}, nil
}
func (f *fakeStore) HotelList(context.Context) ([]HotelSummary, error) { return nil, nil }
func (f *fakeStore) WishRequests(context.Context, int64) ([]WishRequest, error) {
	return f.wishRequests, nil
}

// LLM не должна подменять подтверждение расчёта другим вопросом: без «верно?» берётся шаблон.
func TestPhraseKeepsConfirmQuestion(t *testing.T) {
	c := seaViewChar()
	h := Hotel{ID: 5, Name: "Hotel Test"}
	d := "По расчёту из номеров «Vista mar» море не видно — верно? (да / нет / уточнить)"
	bad := &fakeLLM{reply: func(string, string) (string, error) {
		return "Насколько хорошо видно море?", nil
	}}
	text, mode := newInterviewer(t, baseStore(), bad).phrase(context.Background(), h, c, d, domain.FormatConfirm)
	if text != d || mode != agent.ModePlanner {
		t.Errorf("подмена вопроса: %q (%s)", text, mode)
	}
	good := &fakeLLM{reply: func(string, string) (string, error) {
		return "Верно ли, что из номеров «Vista mar» море не видно?", nil
	}}
	if text, mode = newInterviewer(t, baseStore(), good).phrase(context.Background(), h, c, d, domain.FormatConfirm); mode != agent.ModeCache || text == d {
		t.Errorf("нормальная переформулировка принимается: %q (%s)", text, mode)
	}
}

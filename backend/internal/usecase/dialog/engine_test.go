package dialog

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
	embedded "github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

// fakeSearch подменяет поиск: выдача зависит только от желаний запроса, как её задал тест.
type fakeSearch struct {
	rank    func(q domain.Query) []int64
	queries []domain.Query
	similar []int64
}

var hotelNames = map[int64]string{1: "Hotel Arts", 2: "Hotel Omm", 3: "Casa Fuster", 4: "Hotel Colonial", 5: "Hotel Oasis",
	6: "Serras Hotel Barcelona", 7: "Hotel Santa Marta", 8: "Hotel Attica 21", 9: "Hotel Catedral"}

func (f *fakeSearch) Search(_ context.Context, q domain.Query) (search.Result, error) {
	f.queries = append(f.queries, q)
	var hs []domain.HotelResult
	for _, id := range f.rank(q) {
		hs = append(hs, domain.HotelResult{HotelID: id, Name: hotelNames[id], Best: domain.Variant{Name: "Стандарт", PriceEUR: 100}})
	}
	if q.Limit > 0 && len(hs) > q.Limit {
		hs = hs[:q.Limit]
	}
	return search.Result{Query: q, Hotels: hs, Groups: domain.BuildGroups(hs, q.Limit), Considered: 9}, nil
}

func (f *fakeSearch) Similar(_ context.Context, id int64, limit int, _ map[string]float64) ([]search.SimilarHotel, error) {
	f.similar = append(f.similar, id)
	return []search.SimilarHotel{{Candidate: search.Candidate{ID: 9, Name: hotelNames[9]}}}, nil
}

func (f *fakeSearch) Hotel(_ context.Context, id int64) (search.HotelCard, error) {
	return search.HotelCard{Candidate: search.Candidate{ID: id, Name: hotelNames[id]}}, nil
}

func (f *fakeSearch) Hotels(context.Context) ([]search.Candidate, error) {
	var out []search.Candidate
	for id := int64(1); id <= 9; id++ {
		out = append(out, search.Candidate{ID: id, Name: hotelNames[id]})
	}
	return out, nil
}

type memSessions map[string][]byte

func (m memSessions) LoadState(_ context.Context, id string) ([]byte, bool, error) {
	b, ok := m[id]
	return b, ok, nil
}
func (m memSessions) SaveState(_ context.Context, id string, b []byte) error { m[id] = b; return nil }

type memUnmet map[string]int

func (m memUnmet) LogUnmet(_ context.Context, _ int64, _, normalized string, _ []byte) (int, error) {
	m[normalized]++
	return m[normalized], nil
}

type memTrace []agent.TraceStep

func (m *memTrace) AppendTrace(_ context.Context, s agent.TraceStep) error {
	*m = append(*m, s)
	return nil
}

type rig struct {
	e      *Engine
	search *fakeSearch
	unmet  memUnmet
	trace  *memTrace
}

func newRig(t *testing.T, rank func(q domain.Query) []int64) *rig {
	t.Helper()
	reg, err := adapterregistry.Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{search: &fakeSearch{rank: rank}, unmet: memUnmet{}, trace: &memTrace{}}
	r.e = &Engine{Registry: reg, Lexicon: domain.NewLexicon(reg), Search: r.search, Sessions: memSessions{}, Unmet: r.unmet, Trace: r.trace, RegionID: 1}
	return r
}

func has(q domain.Query, key string) (domain.Desire, bool) {
	for _, w := range q.Wishes {
		if w.Key == key {
			return w, true
		}
	}
	return domain.Desire{}, false
}

// seaOrBeach — выдача зависит от трактовки «у моря»: вид и пляж ставят разные отели наверх.
func seaOrBeach(q domain.Query) []int64 {
	switch {
	case slices.ContainsFunc(q.Wishes, func(w domain.Desire) bool { return w.Key == "sea_view" }):
		return []int64{1, 2, 3, 4}
	case slices.ContainsFunc(q.Wishes, func(w domain.Desire) bool { return w.Key == "walk_to_beach" }):
		return []int64{4, 5, 6, 1}
	}
	return []int64{1, 2, 3, 4}
}

func turn(t *testing.T, r *rig, req Request) (events []Event) {
	t.Helper()
	if err := r.e.Turn(context.Background(), req, func(ev Event) { events = append(events, ev) }); err != nil {
		t.Fatal(err)
	}
	return events
}

func find[T any](events []Event, typ string) (T, bool) {
	for _, ev := range events {
		if ev.Type == typ {
			v, ok := ev.Data.(T)
			return v, ok
		}
	}
	var zero T
	return zero, false
}

func state(t *testing.T, r *rig, id string) State {
	t.Helper()
	var st State
	raw, _ := r.e.Sessions.(memSessions)[id]
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// «У моря»: топ-3 по трактовкам разный → вопрос с кнопками, поиска нет, спрос пробных запросов не пишется.
func TestClarificationAskedWhenTop3Differs(t *testing.T) {
	r := newRig(t, seaOrBeach)
	ev := turn(t, r, Request{SessionID: "s", Message: "хочу отель у моря"})
	q, ok := find[Clarification](ev, EventClarification)
	if !ok {
		t.Fatalf("ждали уточнение, события: %v", ev)
	}
	if !strings.Contains(q.Question, "вид из окна или пляж рядом (до 10 минут)") || len(q.Options) != 2 {
		t.Errorf("вопрос и кнопки: %+v", q)
	}
	if _, got := find[ResultsEvent](ev, EventResults); got {
		t.Error("пока гость не ответил, выдачи быть не должно")
	}
	for _, sq := range r.search.queries {
		if !sq.NoDemand {
			t.Errorf("пробный поиск пишет спрос: %+v", sq)
		}
	}
	st := state(t, r, "s")
	if st.Asked != 1 || st.Pending == nil || len(st.Wishes) != 0 {
		t.Errorf("состояние после вопроса: %+v", st)
	}

	// Ответ кнопкой: желание входит в запрос, второго вопроса нет, итоговый поиск пишет спрос.
	ev = turn(t, r, Request{SessionID: "s", Choice: "walk_to_beach"})
	res, ok := find[ResultsEvent](ev, EventResults)
	if !ok {
		t.Fatal("после ответа ждали выдачу")
	}
	if _, has := has(res.Query, "walk_to_beach"); !has {
		t.Errorf("выбранная трактовка не попала в запрос: %+v", res.Query.Wishes)
	}
	if last := r.search.queries[len(r.search.queries)-1]; last.NoDemand || last.SessionID != "s" {
		t.Errorf("итоговый поиск должен писать спрос в сессию: %+v", last)
	}
}

// Топ-3 одинаков при любой трактовке → не спрашиваем, берём трактовку по умолчанию (пляж), без допущения в тексте.
func TestClarificationSkippedWhenTop3Same(t *testing.T) {
	r := newRig(t, func(domain.Query) []int64 { return []int64{1, 2, 3, 4} })
	ev := turn(t, r, Request{SessionID: "s", Message: "у моря"})
	if _, asked := find[Clarification](ev, EventClarification); asked {
		t.Fatal("вопрос не нужен: выдача одинакова")
	}
	res, ok := find[ResultsEvent](ev, EventResults)
	if !ok {
		t.Fatal("ждали выдачу")
	}
	if w, ok := has(res.Query, "walk_to_beach"); !ok || w.Value != 10.0 {
		t.Errorf("по умолчанию «у моря» — пляж рядом (до 10 минут): %+v", res.Query.Wishes)
	}
	if _, ok := has(res.Query, "sea_view"); ok {
		t.Error("вид из окна по умолчанию не подставляется")
	}
}

// Не больше двух вопросов на сессию: третий раз выдача различается, но мы продолжаем с названным допущением.
func TestClarificationSessionLimit(t *testing.T) {
	r := newRig(t, seaOrBeach)
	for i, id := range []string{"a", "b"} {
		_ = i
		turn(t, r, Request{SessionID: id, Message: "у моря"})
	}
	st := State{Asked: MaxClarificationsPerSession}
	raw, _ := json.Marshal(st)
	r.e.Sessions.(memSessions)["limit"] = raw
	ev := turn(t, r, Request{SessionID: "limit", Message: "у моря"})
	if _, asked := find[Clarification](ev, EventClarification); asked {
		t.Fatal("лимит сессии исчерпан, вопроса быть не должно")
	}
	msg, _ := find[MessageEvent](ev, EventMessage)
	if !strings.Contains(msg.Text, "я поняла как «пляж рядом (до 10 минут)»") {
		t.Errorf("допущение должно быть названо: %s", msg.Text)
	}
}

// Один вопрос на сообщение: вид/пляж и важность тишины одновременно → спрашиваем про море, про тишину — допущение.
func TestClarificationOnePerMessage(t *testing.T) {
	r := newRig(t, func(q domain.Query) []int64 {
		if w, ok := has(q, "noise_night"); ok && w.Importance == domain.Must {
			return []int64{7, 8, 9}
		}
		return seaOrBeach(q)
	})
	ev := turn(t, r, Request{SessionID: "s", Message: "у моря и нужна тишина"})
	q, ok := find[Clarification](ev, EventClarification)
	if !ok || q.Kind != KindAmbiguity {
		t.Fatalf("ждали вопрос про море: %+v", ev)
	}
	var asks int
	for _, e := range ev {
		if e.Type == EventClarification {
			asks++
		}
	}
	if asks != 1 {
		t.Errorf("вопросов на сообщение %d, ждали 1", asks)
	}
}

// Важность: «нужна тишина» может быть must и prefer; если топ-3 разный — вопрос про важность.
func TestImportanceClarification(t *testing.T) {
	r := newRig(t, func(q domain.Query) []int64 {
		if w, ok := has(q, "noise_night"); ok && w.Importance == domain.Must {
			return []int64{7, 8, 9}
		}
		return []int64{1, 2, 3}
	})
	ev := turn(t, r, Request{SessionID: "s", Message: "нужна тишина"})
	q, ok := find[Clarification](ev, EventClarification)
	if !ok || q.Kind != KindImportance {
		t.Fatalf("ждали вопрос о важности: %+v", ev)
	}
	ev = turn(t, r, Request{SessionID: "s", Choice: "must"})
	res, _ := find[ResultsEvent](ev, EventResults)
	if w, _ := has(res.Query, "noise_night"); w.Importance != domain.Must {
		t.Errorf("ответ «обязательно» не применён: %+v", w)
	}
	// Повторно про ту же характеристику не спрашиваем.
	ev = turn(t, r, Request{SessionID: "s", Message: "нужна тишина"})
	if _, asked := find[Clarification](ev, EventClarification); asked {
		t.Error("важность уже решена")
	}
}

// Желания копятся, «без пляжа» убирает, этаж применяется ко всем желаниям фасада.
func TestSessionAccumulatesAndRemoves(t *testing.T) {
	r := newRig(t, func(domain.Query) []int64 { return []int64{1, 2, 3} })
	turn(t, r, Request{SessionID: "s", Message: "тихий номер, пляж рядом"})
	ev := turn(t, r, Request{SessionID: "s", Message: "и вид на море"})
	res, _ := find[ResultsEvent](ev, EventResults)
	for _, k := range []string{"noise_night", "walk_to_beach", "sea_view"} {
		if _, ok := has(res.Query, k); !ok {
			t.Errorf("желание %s потерялось: %+v", k, res.Query.Wishes)
		}
	}
	ev = turn(t, r, Request{SessionID: "s", Message: "без пляжа"})
	res, _ = find[ResultsEvent](ev, EventResults)
	if _, ok := has(res.Query, "walk_to_beach"); ok {
		t.Error("«без пляжа» должен убрать желание")
	}
	if _, ok := has(res.Query, "sea_view"); !ok {
		t.Error("остальные желания остаются")
	}
	ev = turn(t, r, Request{SessionID: "s", Message: "с 4 этажа"})
	res, _ = find[ResultsEvent](ev, EventResults)
	if w, _ := has(res.Query, "sea_view"); w.MinFloor != 4 {
		t.Errorf("этаж должен примениться к виду на море: %+v", w)
	}
}

// «Похожий на этот» и по номеру в выдаче: профиль, а не поиск; желания сессии остаются.
func TestSimilarByCurrentAndOrdinal(t *testing.T) {
	r := newRig(t, func(domain.Query) []int64 { return []int64{1, 2, 3} })
	turn(t, r, Request{SessionID: "s", Message: "тихий номер"})
	ev := turn(t, r, Request{SessionID: "s", Message: "покажи похожий на этот"})
	res, ok := find[ResultsEvent](ev, EventResults)
	if !ok || res.Kind != "similar" || res.Similar.Base.ID != 1 {
		t.Fatalf("похожие на текущий (первый в выдаче): %+v", res)
	}
	ev = turn(t, r, Request{SessionID: "s", Message: "а теперь похожий на первый"})
	res, _ = find[ResultsEvent](ev, EventResults)
	if res.Kind != "similar" || res.Similar.Base.ID != 9 {
		t.Errorf("номер относится к последней выдаче (теперь это похожие, первый — 9): %+v", res.Similar)
	}
	if st := state(t, r, "s"); len(st.Wishes) != 1 {
		t.Errorf("«похожий» не должен менять желания: %+v", st.Wishes)
	}
}

// Непонятое пишется в unmet_wishes, повтор считается; ответ честно говорит, что не учтено.
func TestUnmetWishLoggedAndCounted(t *testing.T) {
	r := newRig(t, func(domain.Query) []int64 { return []int64{1, 2, 3} })
	ev := turn(t, r, Request{SessionID: "s", Message: "морской воздух"})
	msg, _ := find[MessageEvent](ev, EventMessage)
	if !strings.Contains(msg.Text, "Пока не умею учитывать") {
		t.Errorf("ответ должен признать, что не учтено: %s", msg.Text)
	}
	turn(t, r, Request{SessionID: "t", Message: "воздух морской"})
	if got := r.unmet[domain.NormalizeUnmet("морской воздух")]; got != 2 {
		t.Errorf("повтор в другой форме считается: %v", r.unmet)
	}
	if len(r.search.queries) != 0 {
		t.Error("без понятых желаний поиск не запускается")
	}
}

func TestVerifyRephrase(t *testing.T) {
	source := "Hotel Arts — Стандарт, 120 € за ночь. Пляж: 7 мин, уверенность 85 %. Альтернатива — Hotel Omm."
	shown := []string{"Hotel Arts", "Hotel Omm"}
	all := []string{"Hotel Arts", "Hotel Omm", "Hotel Arts Plaza", "Casa Fuster"}
	cases := []struct {
		text string
		ok   bool
	}{
		{"Hotel Arts за 120 € и до пляжа 7 минут.", true},
		{"Hotel Arts: до пляжа 7,0 минут", true},
		{"Hotel Arts: до пляжа 6 минут", false},
		{"К сожалению, я не нашла подходящих отелей.", false},
		{"Hotel Arts, а ещё Casa Fuster", false},
		{"Hotel Arts Plaza тоже хорош", false},
		{"Hotel Arts — тихий вариант", true},
	}
	for _, c := range cases {
		err := VerifyRephrase(c.text, source, shown, all)
		if (err == nil) != c.ok {
			t.Errorf("%q: ok=%v, err=%v", c.text, c.ok, err)
		}
	}
}

func TestMergeResetAndFilters(t *testing.T) {
	reg, _ := adapterregistry.Load(embedded.FS)
	st := State{Wishes: []domain.Desire{{Key: "pets_allowed"}}, Filters: domain.Filters{PriceMax: 100}}
	st.Merge(reg, domain.ParseResult{Filters: domain.Filters{StarsMin: 4}})
	if st.Filters.PriceMax != 100 || st.Filters.StarsMin != 4 {
		t.Errorf("фильтры дополняют друг друга: %+v", st.Filters)
	}
	st.Merge(reg, domain.ParseResult{Reset: true})
	if len(st.Wishes) != 0 || st.Filters != (domain.Filters{}) {
		t.Errorf("«заново» сбрасывает запрос: %+v", st)
	}
}

// Слова уверенности допустимы, только если для упомянутого отеля в шаблоне есть гарантия.
func TestVerifyIntroCertaintyWords(t *testing.T) {
	source := "Hotel Arts — Стандарт, 100 € за ночь. Тишина: совпало, в категории гарантировано."
	shown := []string{"Hotel Arts", "Hotel Omm"}
	all := append([]string{"Casa Fuster"}, shown...)
	text := "Hotel Arts — тишина там гарантирована."
	if err := VerifyIntro(text, source, shown, all, []string{"Hotel Arts"}); err != nil {
		t.Errorf("гарантия в шаблоне есть, слово допустимо: %v", err)
	}
	if err := VerifyIntro("Hotel Arts полностью соответствует требованиям.", source, shown, all, []string{"Hotel Arts"}); err == nil {
		t.Error("«соответствует требованиям» не допускается даже при гарантии")
	}
	for _, bad := range []string{"Hotel Arts — тишина там гарантирована.", "Hotel Arts точно подойдёт.", "Hotel Arts наверняка тихий."} {
		if err := VerifyIntro(bad, source, shown, all, []string{"Hotel Omm"}); err == nil {
			t.Errorf("%q: гарантии по отелю нет, слово уверенности должно отклоняться", bad)
		}
	}
	if err := VerifyIntro("Hotel Arts подойдёт: до пляжа 7 минут.", source, shown, all, nil); err == nil {
		t.Error("число 7 не из данных")
	}
}

func TestDedupeUnmet(t *testing.T) {
	got := dedupeUnmet([]string{"балкон с джакузи и морской воздух", "балкон с джакузи", "морской воздух"})
	if len(got) != 2 || got[0] != "балкон с джакузи" || got[1] != "морской воздух" {
		t.Errorf("составное покрыто частями: %v", got)
	}
	if got := dedupeUnmet([]string{"балкон с джакузи и морской воздух", "балкон"}); len(got) != 2 {
		t.Errorf("часть не покрывает составное: %v", got)
	}
}

// Нет отелей «всё сразу» — шаблон говорит об этом сам (из структуры оценок) и строит блоки компромиссов.
func TestResultsPartsCompromiseBlocks(t *testing.T) {
	r := newRig(t, same)
	mk := func(id int64, score, minutes float64) domain.HotelResult {
		return domain.HotelResult{HotelID: id, Name: hotelNames[id], Best: domain.Variant{Name: "Стандарт", PriceEUR: 90, Score: score, Evals: []domain.WishEval{
			{Key: "noise_night", Status: domain.StatusMatch, Importance: domain.Prefer, Confidence: 0.9},
			{Key: "walk_to_beach", Status: domain.StatusMismatch, Importance: domain.Prefer, Value: minutes, Confidence: 0.7},
		}}}
	}
	hs := []domain.HotelResult{mk(1, 0.7, 18), mk(2, 0.6, 44)}
	res := search.Result{Hotels: hs, Groups: domain.BuildGroups(hs, 5), Query: domain.Query{Wishes: []domain.Desire{
		{Key: "noise_night", Importance: domain.Prefer}, {Key: "walk_to_beach", Importance: domain.Prefer}}}}
	_, rest := r.e.resultsParts(res, nil, nil, nil)
	for _, want := range []string{
		"Отелей, где есть всё сразу, не нашлось: тишина ночью и пешком до пляжа вместе не встречаются. Варианты:",
		"Тишина ночью, но пешком до пляжа от 18 мин:",
		"1. Hotel Arts — Стандарт",
		"2. Hotel Omm — Стандарт",
	} {
		if !strings.Contains(rest, want) {
			t.Errorf("в ответе нет %q:\n%s", want, rest)
		}
	}
	// Вводная модели не может отменить факт «полного совпадения нет»: он в строках кода.
	text, src := r.e.finalText(context.Background(), &tracer{ctx: context.Background(), e: r.e, emit: func(Event) {}}, "Нашла Hotel Arts.", "Ищу.", rest, "", []string{"Hotel Arts", "Hotel Omm"}, nil)
	if src != "llm" || !strings.Contains(text, "не нашлось") {
		t.Errorf("строки кода остаются при вводной модели: %s %s", src, text)
	}
}

package dialog

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Option — кнопка уточнения.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Clarification — вопрос гостю с кнопками. Ответ приходит в запросе как choice = id кнопки.
type Clarification struct {
	Kind     string   `json:"kind"`
	Phrase   string   `json:"phrase"`
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

// topN — сколько первых отелей сравнивает правило уточнения.
const topN = 3

// differ запускает поиск по каждой трактовке (пробный, спрос не пишется) и сообщает, различается ли топ-3.
// Сравнивается упорядоченный список: другой первый отель — уже другая выдача для гостя.
func (e *Engine) differ(ctx context.Context, st *State, base []domain.Desire, desires []domain.Desire) (bool, [][]int64, error) {
	var tops [][]int64
	for _, d := range desires {
		res, err := e.Search.Search(ctx, domain.Query{
			Wishes: upsert(base, st.withFloor(e.Registry, d)), Filters: st.Filters, Limit: topN, NoDemand: true,
		})
		if err != nil {
			return false, nil, err
		}
		ids := make([]int64, 0, topN)
		for _, h := range res.Hotels {
			ids = append(ids, h.HotelID)
		}
		tops = append(tops, ids)
	}
	for _, t := range tops[1:] {
		if !slices.Equal(t, tops[0]) {
			return true, tops, nil
		}
	}
	return false, tops, nil
}

func ambiguityQuestion(a domain.Ambiguity) Clarification {
	labels := make([]string, len(a.Options))
	opts := make([]Option, len(a.Options))
	for i, o := range a.Options {
		labels[i] = o.Label
		opts[i] = Option{ID: o.Key, Label: o.Label}
	}
	return Clarification{
		Kind: KindAmbiguity, Phrase: a.Phrase, Options: opts,
		Question: fmt.Sprintf("«%s»: важен %s?", capitalize(a.Phrase), strings.Join(labels, " или ")),
	}
}

func importanceQuestion(title, key string) Clarification {
	return Clarification{
		Kind: KindImportance, Phrase: key, Question: fmt.Sprintf("«%s» — это обязательное условие или пожелание?", title),
		Options: []Option{{ID: string(domain.Must), Label: "Обязательно"}, {ID: string(domain.Prefer), Label: "Желательно"}},
	}
}

func capitalize(s string) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return s
	}
	rs[0] = []rune(strings.ToUpper(string(rs[0])))[0]
	return string(rs)
}

// defaultOption — трактовка по умолчанию (в реестре ровно одна на фразу).
func defaultOption(a domain.Ambiguity) domain.Interpretation {
	for _, o := range a.Options {
		if o.Default {
			return o
		}
	}
	return a.Options[0]
}

// clarifier считает вопросы, заданные в одном сообщении.
type clarifier struct{ asked int }

func (c *clarifier) canAsk(st *State) bool {
	return c.asked < MaxClarificationsPerMessage && st.Asked < MaxClarificationsPerSession
}

// clarifyAmbiguity применяет правило уточнения к одной неоднозначной фразе.
//
// В запрос идёт трактовка по умолчанию, пока гость не ответил. Спрашиваем, только если выдача по трактовкам
// различается в топ-3, не больше одного вопроса на сообщение и двух на сессию; иначе продолжаем с допущением,
// которое называем гостю (если оно повлияло бы на выдачу). Вопрос возвращается, когда задан; note — допущение.
func (e *Engine) clarifyAmbiguity(ctx context.Context, st *State, amb domain.Ambiguity, c *clarifier, tr *tracer) (ask *Clarification, note string, err error) {
	def := defaultOption(amb)
	desires := make([]domain.Desire, len(amb.Options))
	for i, o := range amb.Options {
		desires[i] = o.Desire
	}
	diff, tops, err := e.differ(ctx, st, st.Wishes, desires)
	if err != nil {
		return nil, "", err
	}
	tr.step("сравниваю выдачу по трактовкам «"+amb.Phrase+"»", "compare_interpretations",
		map[string]any{"phrase": amb.Phrase, "options": optionKeys(amb)}, map[string]any{"differs": diff, "top3": tops})
	switch {
	case diff && c.canAsk(st):
		q := ambiguityQuestion(amb)
		st.Pending = &Pending{Kind: KindAmbiguity, Phrase: amb.Phrase, Options: amb.Options}
		st.Asked++
		c.asked++
		tr.step("задаю уточнение: выдача по трактовкам различается", "ask_clarification",
			map[string]any{"phrase": amb.Phrase}, map[string]any{"question": q.Question})
		return &q, "", nil
	case diff:
		note = fmt.Sprintf("«%s» я поняла как «%s»; если нужно иначе — скажите.", amb.Phrase, def.Label)
		tr.step("уточнение не задаю: лимит исчерпан, продолжаю с допущением", "assume",
			map[string]any{"phrase": amb.Phrase}, map[string]any{"assumed": def.Label, "asked_in_session": st.Asked})
	default:
		tr.step("уточнение не нужно: топ-3 не меняется", "assume",
			map[string]any{"phrase": amb.Phrase}, map[string]any{"assumed": def.Label})
	}
	st.Wishes = upsert(st.Wishes, st.withFloor(e.Registry, def.Desire))
	return nil, note, nil
}

// clarifyImportance — то же для мягкого маркера важности («нужна тишина»: обязательно или желательно).
func (e *Engine) clarifyImportance(ctx context.Context, st *State, key string, c *clarifier, tr *tracer) (ask *Clarification, note string, err error) {
	w, ok := st.wish(key)
	if !ok || slices.Contains(st.Settled, key) {
		return nil, "", nil
	}
	st.settle(key)
	other := withImportance(w, domain.Must)
	if w.Importance == domain.Must {
		other = withImportance(w, domain.Prefer)
	}
	diff, tops, err := e.differ(ctx, st, st.Wishes, []domain.Desire{w, other})
	if err != nil {
		return nil, "", err
	}
	tr.step("сравниваю выдачу при разной важности «"+key+"»", "compare_interpretations",
		map[string]any{"key": key}, map[string]any{"differs": diff, "top3": tops})
	if !diff {
		return nil, "", nil
	}
	title := key
	if ch, ok := e.Registry.Get(key); ok {
		title = ch.Title
	}
	if !c.canAsk(st) {
		return nil, fmt.Sprintf("«%s» я считаю пожеланием, а не обязательным условием; если нужно жёстко — скажите.", title), nil
	}
	q := importanceQuestion(title, key)
	st.Pending = &Pending{Kind: KindImportance, Phrase: key, Options: []domain.Interpretation{
		{Key: string(domain.Must), Label: "Обязательно", Desire: withImportance(w, domain.Must)},
		{Key: string(domain.Prefer), Label: "Желательно", Desire: withImportance(w, domain.Prefer), Default: true},
	}}
	st.Asked++
	c.asked++
	// Пока гость не ответил, желание остаётся мягким.
	st.Wishes = upsert(st.Wishes, withImportance(w, domain.Prefer))
	tr.step("задаю уточнение о важности: выдача различается", "ask_clarification",
		map[string]any{"key": key}, map[string]any{"question": q.Question})
	return &q, "", nil
}

// resolveAmbiguities прогоняет через правило все неоднозначные фразы и мягкую важность сообщения: первый
// различающийся вопрос задаётся, остальные фразы берутся по умолчанию.
func (e *Engine) resolveAmbiguities(ctx context.Context, st *State, amb []domain.Ambiguity, weak []string, tr *tracer) (*Clarification, []string, error) {
	var ask *Clarification
	var notes []string
	c := &clarifier{}
	for _, a := range amb {
		q, note, err := e.clarifyAmbiguity(ctx, st, a, c, tr)
		if err != nil {
			return nil, nil, err
		}
		if q != nil {
			ask = q
		}
		if note != "" {
			notes = append(notes, note)
		}
	}
	for _, key := range weak {
		q, note, err := e.clarifyImportance(ctx, st, key, c, tr)
		if err != nil {
			return nil, nil, err
		}
		if q != nil {
			ask = q
		}
		if note != "" {
			notes = append(notes, note)
		}
	}
	return ask, notes, nil
}

func withImportance(d domain.Desire, imp domain.Importance) domain.Desire {
	d.Importance = imp
	return d
}

func optionKeys(a domain.Ambiguity) []string {
	keys := make([]string, len(a.Options))
	for i, o := range a.Options {
		keys[i] = o.Key
	}
	return keys
}

// answerPending применяет ответ на висящий вопрос: кнопку (choice) или трактовку по умолчанию, если гость ответил
// другой фразой. Возвращает пояснение для журнала.
func (e *Engine) answerPending(st *State, choice string) string {
	p := st.Pending
	st.Pending = nil
	if p == nil || len(p.Options) == 0 {
		return ""
	}
	pick := p.Options[0]
	for _, o := range p.Options {
		if o.Default {
			pick = o
		}
	}
	by := "допущение"
	for _, o := range p.Options {
		if choice != "" && o.Key == choice {
			pick, by = o, "ответ гостя"
		}
	}
	st.Wishes = upsert(st.Wishes, st.withFloor(e.Registry, pick.Desire))
	if p.Kind == KindImportance {
		st.settle(p.Phrase)
	}
	return fmt.Sprintf("%s: «%s» → %s", by, p.Phrase, pick.Label)
}

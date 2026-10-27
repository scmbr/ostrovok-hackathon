package dialog

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// AgentName — имя агента диалога в agent_trace.
const AgentName = "guest_dialog"

// Режимы журнала: rule — решение принял код, llm / cache — выбор сделала модель (живая или из кеша).
const (
	ModeRule  = "rule"
	ModeLLM   = "llm"
	ModeCache = "cache"
)

// Типы событий потока ответа (SSE).
const (
	EventStep          = "step"
	EventClarification = "clarification"
	EventResults       = "results"
	EventMessage       = "message"
	EventDone          = "done"
)

// Event — событие ответа на одно сообщение гостя. Data — одна из структур ниже.
type Event struct {
	Type string
	Data any
}

// StepEvent — решение агента, то же, что пишется в agent_trace.
type StepEvent struct {
	N        int            `json:"n"`
	Mode     string         `json:"mode"`
	Decision string         `json:"decision"`
	Tool     string         `json:"tool,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Output   map[string]any `json:"output,omitempty"`
}

// SimilarView — «похожие на этот» вместо поиска.
type SimilarView struct {
	Base   Shown
	Hotels []search.SimilarHotel
}

// ResultsEvent — выдача: поиск (Hotels с объяснениями) или «похожие» (Similar).
type ResultsEvent struct {
	Kind         string // "search" | "similar"
	Query        domain.Query
	Hotels       []domain.HotelResult
	Explanations []domain.HotelExplanation
	Similar      *SimilarView
	Considered   int
	Dropped      int
}

// MessageEvent — текст ответа. Source: "template" — собран кодом, "llm" — пересказ модели, прошедший проверку.
type MessageEvent struct {
	Text   string
	Source string
}

// DoneEvent — конец ответа на сообщение.
type DoneEvent struct {
	SessionID string
	Mode      string
	Asked     int
}

// Request — сообщение гостя.
type Request struct {
	SessionID string
	Message   string
	// Choice — id кнопки ответа на уточнение.
	Choice string
	// HotelID — отель, открытый у гостя сейчас («похожий на этот»).
	HotelID int64
}

// Engine — детерминированное ядро диалога. LLM-агент (agent.go) использует те же функции как инструменты.
type Engine struct {
	Registry  domain.Registry
	Lexicon   domain.Lexicon
	Search    Searcher
	Sessions  SessionStore
	Unmet     UnmetStore
	Trace     TraceWriter
	RegionID  int64
	Mode      string
	Rephraser Rephraser
	// ResultLimit — сколько отелей отдаёт выдача диалога.
	ResultLimit int
}

// tracer ведёт нумерацию шагов одного сообщения, пишет журнал и отправляет события step.
type tracer struct {
	ctx       context.Context
	e         *Engine
	sessionID string
	emit      func(Event)
	n         int
	err       error
}

func (t *tracer) step(decision, tool string, in, out map[string]any) {
	t.n++
	ts := agent.TraceStep{
		Agent: AgentName, SessionID: t.sessionID, Mode: t.e.mode(), Step: t.n,
		Decision: decision, Tool: tool, Input: in, Output: out,
	}
	if t.e.Trace != nil {
		if err := t.e.Trace.AppendTrace(t.ctx, ts); err != nil && t.err == nil {
			t.err = fmt.Errorf("журнал: %w", err)
		}
	}
	t.emit(Event{EventStep, StepEvent{N: t.n, Mode: ts.Mode, Decision: decision, Tool: tool, Input: in, Output: out}})
}

func (e *Engine) mode() string {
	if e.Mode == "" {
		return ModeRule
	}
	return e.Mode
}

func (e *Engine) limit() int {
	if e.ResultLimit > 0 {
		return e.ResultLimit
	}
	return 5
}

func (e *Engine) load(ctx context.Context, id string) (State, error) {
	raw, ok, err := e.Sessions.LoadState(ctx, id)
	if err != nil {
		return State{}, fmt.Errorf("сессия: %w", err)
	}
	var st State
	if ok {
		if err := json.Unmarshal(raw, &st); err != nil {
			return State{}, fmt.Errorf("состояние сессии: %w", err)
		}
	}
	return st, nil
}

func (e *Engine) save(ctx context.Context, id string, st State) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return e.Sessions.SaveState(ctx, id, raw)
}

// Turn обрабатывает сообщение гостя целиком в режиме правил: разбор → уточнение или поиск → объяснение.
func (e *Engine) Turn(ctx context.Context, req Request, emit func(Event)) error {
	if req.SessionID == "" {
		return &search.ValidationError{Msg: "нужен session_id"}
	}
	st, err := e.load(ctx, req.SessionID)
	if err != nil {
		return err
	}
	tr := &tracer{ctx: ctx, e: e, sessionID: req.SessionID, emit: emit}
	if req.HotelID != 0 {
		st.Current = req.HotelID
	}

	var notes []string
	if st.Pending != nil {
		if what := e.answerPending(&st, req.Choice); what != "" {
			tr.step("применяю ответ на уточнение", "apply_choice", map[string]any{"choice": req.Choice}, map[string]any{"applied": what})
		}
	}

	p := e.Lexicon.Parse(req.Message)
	tr.step("разбираю фразу детерминированным парсером", "parse_baseline",
		map[string]any{"text": req.Message}, parseSummary(p))
	notes = append(notes, p.Notes...)

	unmet := e.logUnmet(ctx, tr, p.Unmet, req.Message)

	st.Merge(e.Registry, p)
	out, err := e.resolveAmbiguities(ctx, &st, p, tr)
	if err != nil {
		return err
	}
	notes = append(notes, out.Notes...)

	if out.Ask != nil {
		if err := e.save(ctx, req.SessionID, st); err != nil {
			return err
		}
		emit(Event{EventClarification, *out.Ask})
		emit(Event{EventDone, DoneEvent{SessionID: req.SessionID, Mode: e.mode(), Asked: st.Asked}})
		return tr.err
	}

	if p.Similar != nil {
		return e.similar(ctx, req, &st, p, tr, notes, unmet, emit)
	}
	return e.searchAndExplain(ctx, req, &st, p, tr, notes, unmet, emit)
}

func parseSummary(p domain.ParseResult) map[string]any {
	keys := make([]string, 0, len(p.Wishes))
	for _, w := range p.Wishes {
		keys = append(keys, w.Key)
	}
	return map[string]any{
		"wishes": keys, "remove": p.Remove, "ambiguities": len(p.Ambiguities), "weak": p.WeakKeys,
		"unmet": p.Unmet, "filters": p.Filters, "min_floor": p.MinFloor, "reset": p.Reset,
	}
}

// logUnmet записывает непонятые куски в unmet_wishes; возвращает их для ответа гостю.
func (e *Engine) logUnmet(ctx context.Context, tr *tracer, phrases []string, message string) []string {
	var logged []string
	for _, ph := range phrases {
		norm := domain.NormalizeUnmet(ph)
		if norm == "" || e.Unmet == nil {
			continue
		}
		count, err := e.Unmet.LogUnmet(ctx, e.RegionID, ph, norm, nil)
		if err != nil {
			if tr.err == nil {
				tr.err = fmt.Errorf("unmet_wishes: %w", err)
			}
			continue
		}
		logged = append(logged, ph)
		tr.step("желания нет в реестре — записываю в журнал спроса", "log_unmet_wish",
			map[string]any{"text": ph}, map[string]any{"normalized": norm, "count": count})
	}
	return logged
}

func (e *Engine) hotelNames(ctx context.Context) ([]search.Candidate, []string, error) {
	cands, err := e.Search.Hotels(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = c.Name
	}
	return cands, names, nil
}

func (e *Engine) searchAndExplain(ctx context.Context, req Request, st *State, p domain.ParseResult, tr *tracer,
	notes, unmet []string, emit func(Event)) error {
	if len(st.Wishes) == 0 && st.Filters == (domain.Filters{}) {
		text := emptyMessage(p, unmet)
		emit(Event{EventMessage, MessageEvent{Text: text, Source: "template"}})
		if err := e.save(ctx, req.SessionID, *st); err != nil {
			return err
		}
		emit(Event{EventDone, DoneEvent{SessionID: req.SessionID, Mode: e.mode(), Asked: st.Asked}})
		return tr.err
	}
	res, err := e.Search.Search(ctx, domain.Query{
		Wishes: st.Wishes, Filters: st.Filters, SessionID: req.SessionID, Limit: e.limit(),
	})
	if err != nil {
		return err
	}
	tr.step("ищу отели по накопленным желаниям", "search",
		map[string]any{"wishes": wishKeys(st.Wishes), "filters": st.Filters},
		map[string]any{"found": len(res.Hotels), "considered": res.Considered})

	exps := make([]domain.HotelExplanation, 0, len(res.Hotels))
	st.Last = st.Last[:0]
	for i, h := range res.Hotels {
		exps = append(exps, domain.ExplainHotel(e.Registry, i+1, h))
		st.Last = append(st.Last, Shown{ID: h.HotelID, Name: h.Name})
	}
	if len(st.Last) > 0 {
		st.Current = st.Last[0].ID
	}
	emit(Event{EventResults, ResultsEvent{Kind: "search", Query: res.Query, Hotels: res.Hotels, Explanations: exps,
		Considered: res.Considered, Dropped: res.Dropped}})

	template := e.resultsText(res, exps, notes, unmet)
	var shown []string
	for _, h := range res.Hotels {
		shown = append(shown, h.Name)
	}
	text, src := e.finalText(ctx, tr, template, req.Message+" "+strings.Join(notes, " "), shown)
	emit(Event{EventMessage, MessageEvent{Text: text, Source: src}})
	if err := e.save(ctx, req.SessionID, *st); err != nil {
		return err
	}
	emit(Event{EventDone, DoneEvent{SessionID: req.SessionID, Mode: e.mode(), Asked: st.Asked}})
	return tr.err
}

func wishKeys(ws []domain.Desire) []string {
	keys := make([]string, len(ws))
	for i, w := range ws {
		keys[i] = w.Key
	}
	return keys
}

// finalText отдаёт пересказ модели, если он прошёл проверку чисел и названий; иначе шаблон, а причина отказа
// пишется в журнал.
func (e *Engine) finalText(ctx context.Context, tr *tracer, template, extraSource string, shown []string) (string, string) {
	if e.Rephraser == nil {
		return template, "template"
	}
	_, all, err := e.hotelNames(ctx)
	if err != nil {
		return template, "template"
	}
	text, err := e.Rephraser.Rephrase(ctx, template)
	if err != nil {
		tr.step("пересказ моделью недоступен — отдаю шаблон", "rephrase", nil, map[string]any{"error": err.Error()})
		return template, "template"
	}
	if err := VerifyRephrase(text, template+" "+extraSource, shown, all); err != nil {
		tr.step("текст модели отклонён проверкой — отдаю шаблон", "verify_text",
			map[string]any{"text": text}, map[string]any{"reason": err.Error()})
		return template, "template"
	}
	tr.step("пересказ модели прошёл проверку чисел и названий", "verify_text", nil, map[string]any{"ok": true})
	return text, "llm"
}

func emptyMessage(p domain.ParseResult, unmet []string) string {
	if len(unmet) > 0 {
		return fmt.Sprintf("Пока не умею учитывать: %s. Записала это в журнал пожеланий — такие запросы подскажут, какие характеристики добавить. "+
			"Расскажите, что ещё важно: тишина, вид на море, пляж, метро, питомцы, бюджет.", quoted(unmet))
	}
	if len(p.Remove) > 0 {
		return "Убрала это из условий. Что ещё важно в отеле?"
	}
	return "Расскажите, какой отель нужен: тишина, вид на море, пляж, метро, питомцы, бюджет."
}

func quoted(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = "«" + s + "»"
	}
	return strings.Join(q, ", ")
}

// understood — одна строка: какие желания сейчас в запросе.
func (e *Engine) understood(q domain.Query) string {
	var parts []string
	for _, w := range q.Wishes {
		title := w.Key
		if c, ok := e.Registry.Get(w.Key); ok {
			title = c.Title
		}
		mark := "желательно"
		if w.Importance == domain.Must {
			mark = "обязательно"
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", strings.ToLower(title), mark))
	}
	if q.Filters.PriceMax > 0 {
		parts = append(parts, fmt.Sprintf("до %d € за ночь", q.Filters.PriceMax))
	}
	if q.Filters.StarsMin > 0 {
		parts = append(parts, fmt.Sprintf("от %d звёзд", q.Filters.StarsMin))
	}
	return strings.Join(parts, "; ")
}

func (e *Engine) resultsText(res search.Result, exps []domain.HotelExplanation, notes, unmet []string) string {
	var b strings.Builder
	b.WriteString("Ищу: " + e.understood(res.Query) + ".")
	for _, n := range notes {
		if n != "" {
			b.WriteString("\n" + n)
		}
	}
	if len(unmet) > 0 {
		fmt.Fprintf(&b, "\nПока не умею учитывать: %s — записала в журнал пожеланий.", quoted(unmet))
	}
	if len(exps) == 0 {
		b.WriteString("\nПодходящих отелей не нашлось: попробуйте смягчить условия.")
		return b.String()
	}
	fmt.Fprintf(&b, "\nЛучшие варианты:")
	for i, x := range exps {
		if i >= topN {
			break
		}
		b.WriteString("\n" + x.Text())
	}
	return b.String()
}

// similar отвечает на «похожий на …»: профиль фактов отеля, веса — из накопленных желаний.
func (e *Engine) similar(ctx context.Context, req Request, st *State, p domain.ParseResult, tr *tracer,
	notes, unmet []string, emit func(Event)) error {
	cands, _, err := e.hotelNames(ctx)
	if err != nil {
		return err
	}
	base, ok := resolveSimilar(*p.Similar, *st, cands)
	if !ok {
		emit(Event{EventMessage, MessageEvent{Source: "template",
			Text: "Не поняла, на какой отель опереться. Назовите отель из выдачи по номеру или названию."}})
		if err := e.save(ctx, req.SessionID, *st); err != nil {
			return err
		}
		emit(Event{EventDone, DoneEvent{SessionID: req.SessionID, Mode: e.mode(), Asked: st.Asked}})
		return tr.err
	}
	list, err := e.Search.Similar(ctx, base.ID, e.limit(), domain.QueryWeights(st.Wishes))
	if err != nil {
		return err
	}
	tr.step("ищу отели с похожим профилем фактов", "similar",
		map[string]any{"hotel_id": base.ID}, map[string]any{"found": len(list)})
	st.Current = base.ID
	st.Last = st.Last[:0]
	for _, s := range list {
		st.Last = append(st.Last, Shown{ID: s.ID, Name: s.Name})
	}
	emit(Event{EventResults, ResultsEvent{Kind: "similar", Query: domain.Query{Wishes: st.Wishes, Filters: st.Filters},
		Similar: &SimilarView{Base: base, Hotels: list}}})

	template := e.similarText(base, list, notes, unmet)
	shown := []string{base.Name}
	for _, s := range list {
		shown = append(shown, s.Name)
	}
	text, src := e.finalText(ctx, tr, template, req.Message+" "+strings.Join(notes, " "), shown)
	emit(Event{EventMessage, MessageEvent{Text: text, Source: src}})
	if err := e.save(ctx, req.SessionID, *st); err != nil {
		return err
	}
	emit(Event{EventDone, DoneEvent{SessionID: req.SessionID, Mode: e.mode(), Asked: st.Asked}})
	return tr.err
}

// resolveSimilar находит отель, на который сослался гость: номер в выдаче, «этот» или название.
func resolveSimilar(ref domain.SimilarRef, st State, cands []search.Candidate) (Shown, bool) {
	switch {
	case ref.Ordinal > 0:
		if ref.Ordinal <= len(st.Last) {
			return st.Last[ref.Ordinal-1], true
		}
	case ref.Current:
		if st.Current != 0 {
			for _, c := range cands {
				if c.ID == st.Current {
					return Shown{c.ID, c.Name}, true
				}
			}
		}
		if len(st.Last) > 0 {
			return st.Last[0], true
		}
	case ref.Name != "":
		words := domain.PhraseWords(ref.Name)
		match := func(name string) bool {
			nw := domain.PhraseWords(name)
			for _, w := range words {
				if !slices.ContainsFunc(nw, func(n string) bool { return len([]rune(w)) >= 3 && strings.HasPrefix(n, w) }) {
					return false
				}
			}
			return len(words) > 0
		}
		for _, s := range st.Last {
			if match(s.Name) {
				return s, true
			}
		}
		var found []Shown
		for _, c := range cands {
			if match(c.Name) {
				found = append(found, Shown{c.ID, c.Name})
			}
		}
		if len(found) == 1 {
			return found[0], true
		}
	}
	return Shown{}, false
}

func (e *Engine) similarText(base Shown, list []search.SimilarHotel, notes, unmet []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Похожие на %s — по профилю собранных характеристик, без учёта звёзд и цены:", base.Name)
	for _, n := range notes {
		if n != "" {
			b.WriteString("\n" + n)
		}
	}
	if len(list) == 0 {
		b.WriteString("\nПохожих отелей не нашлось.")
	}
	for i, s := range list {
		if i >= topN {
			break
		}
		fmt.Fprintf(&b, "\n%d. %s", i+1, s.Name)
		if diff := e.mainDifferences(s); diff != "" {
			b.WriteString(" — главное отличие: " + diff)
		}
	}
	if len(unmet) > 0 {
		fmt.Fprintf(&b, "\nПока не умею учитывать: %s — записала в журнал пожеланий.", quoted(unmet))
	}
	return b.String()
}

// mainDifferences — названия двух характеристик с наибольшим различием (числа не показываем: это не факты).
func (e *Engine) mainDifferences(s search.SimilarHotel) string {
	var titles []string
	for _, d := range s.Dims {
		if d.Unknown || d.Distance <= 0.05 {
			continue
		}
		if c, ok := e.Registry.Get(d.Key); ok {
			titles = append(titles, strings.ToLower(c.Title))
		}
		if len(titles) == 2 {
			break
		}
	}
	return strings.Join(titles, ", ")
}

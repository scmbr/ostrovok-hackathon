package dialog

import (
	"context"
	"encoding/json"
	"errors"
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
	// Groups — выдача по группам: «всё сразу» и блоки компромиссов; Hotels — тот же поиск плоским списком.
	Groups     []domain.Group
	Similar    *SimilarView
	Considered int
	Dropped    int
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

// Engine — ядро диалога. Без Chat работает по правилам (парсер, правило уточнения, шаблоны); с Chat выбор шагов
// делает модель, но уточнение, поиск и проверка текста остаются в коде (agent.go).
type Engine struct {
	Registry domain.Registry
	Lexicon  domain.Lexicon
	Search   Searcher
	Sessions SessionStore
	Unmet    UnmetStore
	Trace    TraceWriter
	RegionID int64
	// Chat — LLM с tool calling (живая, с записью или из кеша); nil — только правила. ChatMode — llm | cache.
	Chat     agent.Chat
	ChatMode string
	// ResultLimit — сколько отелей отдаёт выдача диалога.
	ResultLimit int
}

// tracer ведёт нумерацию шагов одного сообщения, пишет журнал и отправляет события step.
type tracer struct {
	ctx       context.Context
	e         *Engine
	sessionID string
	mode      string
	emit      func(Event)
	n         int
	err       error
}

func (t *tracer) step(decision, tool string, in, out map[string]any) {
	t.n++
	ts := agent.TraceStep{
		Agent: AgentName, SessionID: t.sessionID, Mode: t.mode, Step: t.n,
		Decision: decision, Tool: tool, Input: in, Output: out,
	}
	if t.e.Trace != nil {
		if err := t.e.Trace.AppendTrace(t.ctx, ts); err != nil && t.err == nil {
			t.err = fmt.Errorf("журнал: %w", err)
		}
	}
	t.emit(Event{EventStep, StepEvent{N: t.n, Mode: ts.Mode, Decision: decision, Tool: tool, Input: in, Output: out}})
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

// searchOutcome — итог поиска для ответа.
type searchOutcome struct {
	res  search.Result
	exps []domain.HotelExplanation
}

type similarOutcome struct {
	base Shown
	list []search.SimilarHotel
}

// run — один ход диалога: состояние сессии, накопленный разбор, итоги поиска. Правила и LLM-агент
// действуют через одни и те же методы.
type run struct {
	e   *Engine
	ctx context.Context
	req Request
	st  State
	tr  *tracer

	// Разбор сообщения, ещё не доведённый до запроса.
	amb     []domain.Ambiguity
	weak    []string
	simRef  *domain.SimilarRef
	notes   []string
	parsed  bool
	removed []string
	// applied — что сделал ответ гостя на предыдущее уточнение.
	applied string

	// Непонятое: тексты в порядке появления; нормализация и предложение модели, если она их дала.
	unmet     []string
	unmetNorm map[string]string
	proposals map[string][]byte

	clar clarifier
	ask  *Clarification
	srch *searchOutcome
	sim  *similarOutcome
}

func (e *Engine) newRun(ctx context.Context, req Request, mode string, emit func(Event)) (*run, error) {
	if req.SessionID == "" {
		return nil, &search.ValidationError{Msg: "нужен session_id"}
	}
	st, err := e.load(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	if req.HotelID != 0 {
		st.Current = req.HotelID
	}
	return &run{e: e, ctx: ctx, req: req, st: st, tr: &tracer{ctx: ctx, e: e, sessionID: req.SessionID, mode: mode, emit: emit},
		unmetNorm: map[string]string{}, proposals: map[string][]byte{}}, nil
}

// applyPending применяет ответ гостя на висящее уточнение (кнопка или трактовка по умолчанию).
func (r *run) applyPending() {
	if r.st.Pending == nil {
		return
	}
	if what := r.e.answerPending(&r.st, r.req.Choice); what != "" {
		r.applied = what
		r.tr.step("применяю ответ на уточнение", "apply_choice", map[string]any{"choice": r.req.Choice}, map[string]any{"applied": what})
	}
}

// parse разбирает текст детерминированным парсером и вносит понятое в состояние сессии.
func (r *run) parse(text string) domain.ParseResult {
	p := r.e.Lexicon.Parse(text)
	r.parsed = true
	r.tr.step("разбираю фразу детерминированным парсером", "parse_baseline", map[string]any{"text": text}, parseSummary(p))
	r.st.Merge(r.e.Registry, p)
	r.amb = append(r.amb, p.Ambiguities...)
	for _, k := range p.WeakKeys {
		if !slices.Contains(r.weak, k) {
			r.weak = append(r.weak, k)
		}
	}
	if p.Similar != nil {
		r.simRef = p.Similar
	}
	r.notes = append(r.notes, p.Notes...)
	r.removed = append(r.removed, p.Remove...)
	for _, u := range p.Unmet {
		r.queueUnmet(u, "", nil)
	}
	return p
}

// queueUnmet ставит непонятое в очередь журнала; запись — в конце хода, чтобы сбой LLM не дублировал счётчик.
func (r *run) queueUnmet(text, normalized string, proposal []byte) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	key := domain.NormalizeUnmet(text)
	if normalized != "" {
		key = normalized
	}
	if key == "" {
		return
	}
	for _, t := range r.unmet {
		if strings.EqualFold(t, text) || r.unmetNorm[t] == key {
			// То же желание, уже поставленное разбором: нормализация и предложение модели уточняют запись.
			if normalized != "" {
				r.unmetNorm[t] = key
			}
			if proposal != nil {
				r.proposals[t] = proposal
			}
			return
		}
	}
	r.unmet = append(r.unmet, text)
	r.unmetNorm[text] = key
	if proposal != nil {
		r.proposals[text] = proposal
	}
}

// clarifyAll прогоняет неразобранные неоднозначности через правило уточнения (один вопрос на сообщение).
func (r *run) clarifyAll() error {
	if r.ask != nil || (len(r.amb) == 0 && len(r.weak) == 0) {
		return nil
	}
	ask, notes, err := r.e.resolveAmbiguities(r.ctx, &r.st, r.amb, r.weak, r.tr)
	if err != nil {
		return err
	}
	r.amb, r.weak = nil, nil
	r.ask = ask
	r.notes = append(r.notes, notes...)
	return nil
}

// runSearch ищет по накопленным желаниям и строит объяснение.
func (r *run) runSearch() error {
	res, err := r.e.Search.Search(r.ctx, domain.Query{
		Wishes: r.st.Wishes, Filters: r.st.Filters, SessionID: r.req.SessionID, Limit: r.e.limit(),
	})
	if err != nil {
		return err
	}
	r.tr.step("ищу отели по накопленным желаниям", "search",
		map[string]any{"wishes": wishKeys(r.st.Wishes), "filters": r.st.Filters},
		map[string]any{"found": len(res.Hotels), "considered": res.Considered})
	out := &searchOutcome{res: res}
	r.st.Last = r.st.Last[:0]
	for i, h := range res.Hotels {
		out.exps = append(out.exps, domain.ExplainHotel(r.e.Registry, i+1, h))
		r.st.Last = append(r.st.Last, Shown{ID: h.HotelID, Name: h.Name})
	}
	if len(r.st.Last) > 0 {
		r.st.Current = r.st.Last[0].ID
	}
	r.srch, r.sim = out, nil
	return nil
}

var errNoSimilarBase = errors.New("не понятно, на какой отель опереться")

// runSimilar ищет отели с профилем, близким к названному; веса — из накопленных желаний.
func (r *run) runSimilar(ref domain.SimilarRef) error {
	cands, _, err := r.e.hotelNames(r.ctx)
	if err != nil {
		return err
	}
	base, ok := resolveSimilar(ref, r.st, cands)
	if !ok {
		return errNoSimilarBase
	}
	list, err := r.e.Search.Similar(r.ctx, base.ID, r.e.limit(), domain.QueryWeights(r.st.Wishes))
	if err != nil {
		return err
	}
	r.tr.step("ищу отели с похожим профилем фактов", "similar",
		map[string]any{"hotel_id": base.ID}, map[string]any{"found": len(list)})
	r.st.Current = base.ID
	r.st.Last = r.st.Last[:0]
	for _, s := range list {
		r.st.Last = append(r.st.Last, Shown{ID: s.ID, Name: s.Name})
	}
	r.sim, r.srch = &similarOutcome{base: base, list: list}, nil
	return nil
}

// dedupeUnmet убирает составное непонятое, если его слова целиком покрыты более короткими записями («балкон с
// джакузи и морской воздух» при отдельных «балкон с джакузи» и «морской воздух»): модель делит точнее разбора.
func dedupeUnmet(phrases []string) []string {
	sets := make([]map[string]bool, len(phrases))
	for i, p := range phrases {
		sets[i] = map[string]bool{}
		for _, w := range strings.Fields(domain.NormalizeUnmet(p)) {
			sets[i][w] = true
		}
	}
	var out []string
	for i, p := range phrases {
		covered := map[string]bool{}
		for j := range phrases {
			if j != i && len(sets[j]) < len(sets[i]) {
				for w := range sets[j] {
					covered[w] = true
				}
			}
		}
		all := len(sets[i]) > 0 && len(covered) > 0
		for w := range sets[i] {
			if !covered[w] {
				all = false
			}
		}
		if !all {
			out = append(out, p)
		}
	}
	return out
}

// flushUnmet записывает очередь непонятого в unmet_wishes.
func (r *run) flushUnmet() []string {
	var logged []string
	for _, ph := range dedupeUnmet(r.unmet) {
		if r.e.Unmet == nil {
			break
		}
		norm := r.unmetNorm[ph]
		count, err := r.e.Unmet.LogUnmet(r.ctx, r.e.RegionID, ph, norm, r.proposals[ph])
		if err != nil {
			if r.tr.err == nil {
				r.tr.err = fmt.Errorf("unmet_wishes: %w", err)
			}
			continue
		}
		logged = append(logged, ph)
		r.tr.step("желания нет в реестре — записываю в журнал спроса", "log_unmet_wish",
			map[string]any{"text": ph}, map[string]any{"normalized": norm, "count": count})
	}
	return logged
}

// finish завершает ход: записывает непонятое, собирает ответ, сохраняет сессию, отправляет события.
// candidate — текст модели для проверки; пустой — шаблон.
func (r *run) finish(candidate string, emit func(Event)) error {
	unmet := r.flushUnmet()
	done := func() error {
		if err := r.e.save(r.ctx, r.req.SessionID, r.st); err != nil {
			return err
		}
		emit(Event{EventDone, DoneEvent{SessionID: r.req.SessionID, Mode: r.tr.mode, Asked: r.st.Asked}})
		return r.tr.err
	}
	if r.ask != nil {
		emit(Event{EventClarification, *r.ask})
		return done()
	}
	msg := func(text, src string) { emit(Event{EventMessage, MessageEvent{Text: text, Source: src}}) }
	switch {
	case r.sim != nil:
		emit(Event{EventResults, ResultsEvent{Kind: "similar", Query: domain.Query{Wishes: r.st.Wishes, Filters: r.st.Filters},
			Similar: &SimilarView{Base: r.sim.base, Hotels: r.sim.list}}})
		lead, rest := r.e.similarParts(r.sim.base, r.sim.list, r.notes, unmet)
		shown := []string{r.sim.base.Name}
		for _, s := range r.sim.list {
			shown = append(shown, s.Name)
		}
		text, src := r.e.finalText(r.ctx, r.tr, candidate, lead, rest, r.req.Message+" "+strings.Join(r.notes, " "), shown, nil)
		msg(text, src)
	case r.srch != nil:
		o := r.srch
		emit(Event{EventResults, ResultsEvent{Kind: "search", Query: o.res.Query, Hotels: o.res.Hotels, Explanations: o.exps, Groups: o.res.Groups,
			Considered: o.res.Considered, Dropped: o.res.Dropped}})
		lead, rest := r.e.resultsParts(o.res, o.exps, r.notes, unmet)
		shown, guaranteed := shownAndGuaranteed(r.e.textHotels(o.res))
		text, src := r.e.finalText(r.ctx, r.tr, candidate, lead, rest, r.req.Message+" "+strings.Join(r.notes, " "), shown, guaranteed)
		msg(text, src)
	case r.simRef != nil:
		msg("Не поняла, на какой отель опереться. Назовите отель из выдачи по номеру или названию.", "template")
	default:
		msg(emptyMessage(r.removed, unmet), "template")
	}
	return done()
}

// Turn обрабатывает сообщение гостя. С Chat выбор шагов делает модель; при её недоступности или промахе кеша
// ход повторяется по правилам (детерминированный разбор и шаблонные тексты).
func (e *Engine) Turn(ctx context.Context, req Request, emit func(Event)) error {
	if e.Chat != nil {
		handled, err := e.turnLLM(ctx, req, emit)
		if handled || err != nil {
			return err
		}
	}
	return e.turnRules(ctx, req, emit)
}

// turnRules — ход без модели: разбор → уточнение или поиск → объяснение шаблоном.
func (e *Engine) turnRules(ctx context.Context, req Request, emit func(Event)) error {
	r, err := e.newRun(ctx, req, ModeRule, emit)
	if err != nil {
		return err
	}
	r.applyPending()
	r.parse(req.Message)
	if err := r.clarifyAll(); err != nil {
		return err
	}
	if r.ask == nil {
		switch {
		case r.simRef != nil:
			if err := r.runSimilar(*r.simRef); err != nil && !errors.Is(err, errNoSimilarBase) {
				return err
			}
		case len(r.st.Wishes) > 0 || r.st.Filters != (domain.Filters{}):
			if err := r.runSearch(); err != nil {
				return err
			}
		}
	}
	return r.finish("", emit)
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

func wishKeys(ws []domain.Desire) []string {
	keys := make([]string, len(ws))
	for i, w := range ws {
		keys[i] = w.Key
	}
	return keys
}

// guaranteeMark — как шаблон объяснения называет гарантию в категории; только при ней вводная может говорить о гарантии.
const guaranteeMark = "гарантировано"

// finalText собирает ответ: вводная (lead) и всё остальное (rest: допущения, непонятое, отели со статусами,
// значениями, оговорками и источниками) строятся кодом. Модель может заменить только вводную (candidate), и то после
// проверки (VerifyIntro); при отказе остаётся шаблонная вводная, а причина пишется в журнал.
func (e *Engine) finalText(ctx context.Context, tr *tracer, candidate, lead, rest, extraSource string, shown, guaranteed []string) (string, string) {
	template := lead + rest
	if strings.TrimSpace(candidate) == "" {
		return template, "template"
	}
	_, all, err := e.hotelNames(ctx)
	if err != nil {
		return template, "template"
	}
	if err := VerifyIntro(candidate, template+" "+extraSource, shown, all, guaranteed); err != nil {
		tr.step("вводная модели отклонена проверкой — отдаю шаблонную", "verify_text",
			map[string]any{"text": candidate}, map[string]any{"reason": err.Error()})
		return template, "template"
	}
	tr.step("вводная модели прошла проверку", "verify_text", nil, map[string]any{"ok": true})
	return strings.TrimSpace(candidate) + rest, "llm"
}

func emptyMessage(removed, unmet []string) string {
	if len(unmet) > 0 {
		return fmt.Sprintf("Пока не умею учитывать: %s. Записала это в журнал пожеланий — такие запросы подскажут, какие характеристики добавить. "+
			"Расскажите, что ещё важно: тишина, вид на море, пляж, метро, питомцы, бюджет.", quoted(unmet))
	}
	if len(removed) > 0 {
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

// resultsText — шаблон ответа целиком (для модели в результате инструмента).
func (e *Engine) resultsText(res search.Result, exps []domain.HotelExplanation, notes, unmet []string) string {
	lead, rest := e.resultsParts(res, exps, notes, unmet)
	return lead + rest
}

// Сколько отелей группы попадает в текст ответа (в событие results — все).
const (
	textFullHotels       = topN
	textCompromiseHotels = 2
	textCompromiseBlocks = 2
	textOpenHotels       = 2
)

// textHotel — отель, показанный в тексте ответа, с объяснением.
type textHotel struct {
	group domain.Group
	exp   domain.HotelExplanation
	hotel domain.HotelResult
}

// textHotels выбирает отели для текста по группам; ранги сквозные.
func (e *Engine) textHotels(res search.Result) []textHotel {
	var out []textHotel
	blocks := 0
	for _, g := range res.Groups {
		n := textFullHotels
		switch g.Kind {
		case domain.GroupCompromise:
			if blocks++; blocks > textCompromiseBlocks {
				continue
			}
			n = textCompromiseHotels
		case domain.GroupOpen:
			n = textOpenHotels
		}
		for i, h := range g.Hotels {
			if i >= n {
				break
			}
			out = append(out, textHotel{group: g, hotel: h, exp: domain.ExplainHotel(e.Registry, len(out)+1, h)})
		}
	}
	return out
}

// shownAndGuaranteed — названия показанных отелей и тех, у кого шаблон говорит «гарантировано».
func shownAndGuaranteed(hs []textHotel) (shown, guaranteed []string) {
	for _, t := range hs {
		shown = append(shown, t.hotel.Name)
		if strings.Contains(t.exp.Text(), guaranteeMark) {
			guaranteed = append(guaranteed, t.hotel.Name)
		}
	}
	return shown, guaranteed
}

// resultsParts делит ответ на вводную («Ищу: …», её может заменить модель) и остальное (строит только код).
// Если отелей, где есть всё сразу, мало, текст идёт блоками компромиссов с заголовками из структуры оценок.
func (e *Engine) resultsParts(res search.Result, exps []domain.HotelExplanation, notes, unmet []string) (lead, rest string) {
	lead = "Ищу: " + e.understood(res.Query) + "."
	var b strings.Builder
	for _, n := range notes {
		if n != "" {
			b.WriteString("\n" + n)
		}
	}
	if len(unmet) > 0 {
		fmt.Fprintf(&b, "\nПока не умею учитывать: %s — записала в журнал пожеланий.", quoted(unmet))
	}
	if len(res.Hotels) == 0 {
		b.WriteString("\nПодходящих отелей не нашлось: попробуйте смягчить условия.")
		return lead, b.String()
	}
	glead := domain.GroupLead(e.Registry, res.Query.Wishes, res.Groups)
	if glead != "" {
		b.WriteString("\n" + glead)
	} else {
		b.WriteString("\nЛучшие варианты:")
	}
	var prev domain.Group
	for i, t := range e.textHotels(res) {
		if i == 0 || t.group.Kind != prev.Kind || t.group.FailedKey != prev.FailedKey {
			switch {
			case t.group.Kind == domain.GroupCompromise:
				b.WriteString("\n\n" + domain.GroupTitle(e.Registry, t.group) + ":")
			case t.group.Kind == domain.GroupOpen:
				b.WriteString("\n\n" + domain.GroupTitle(e.Registry, t.group) + ":")
			}
		}
		prev = t.group
		b.WriteString("\n" + t.exp.Text())
	}
	return lead, b.String()
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

// similarText — шаблон ответа «похожие» целиком.
func (e *Engine) similarText(base Shown, list []search.SimilarHotel, notes, unmet []string) string {
	lead, rest := e.similarParts(base, list, notes, unmet)
	return lead + rest
}

func (e *Engine) similarParts(base Shown, list []search.SimilarHotel, notes, unmet []string) (lead, rest string) {
	lead = fmt.Sprintf("Похожие на %s — по профилю собранных характеристик, без учёта звёзд и цены:", base.Name)
	var b strings.Builder
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
	return lead, b.String()
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

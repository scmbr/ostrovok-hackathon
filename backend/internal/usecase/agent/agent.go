package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Agent — агент-сборщик. Решает, какие характеристики считать и когда спрашивать отельера; значение факта записать
// не может: инструменты пишут свидетельства, факт собирает движок правды. Без Chat работает детерминированный
// планировщик по порядку стратегий реестра, журнал при этом тот же.
type Agent struct {
	Registry domain.Registry
	Store    Store
	Runner   ToolRunner
	Chat     Chat   // nil — только планировщик
	ChatMode string // ModeLLM или ModeCache: что стоит за Chat
	MaxSteps int    // 0 — MaxSteps
	Now      func() time.Time
}

// Result — итог прогона.
type Result struct {
	SessionID string
	Mode      string // режим в конце прогона
	Steps     int    // записей в журнале
	Actions   int    // запусков инструментов и пересчётов
	Questions int
	Skipped   int
}

type run struct {
	a       *Agent
	h       HotelInfo
	session string
	mode    string
	step    int
	actions int
	asked   int
	skipped int
	ran     map[string]bool
	states  map[string]State
}

// Run выполняет один прогон по отелю.
func (a *Agent) Run(ctx context.Context, hotelID int64) (Result, error) {
	h, err := a.Store.Hotel(ctx, hotelID)
	if err != nil {
		return Result{}, err
	}
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	r := &run{a: a, h: h, session: fmt.Sprintf("%d-%d", h.ID, now().Unix()), ran: map[string]bool{}}
	r.mode = ModePlanner
	if a.Chat != nil {
		r.mode = a.ChatMode
		if r.mode == "" {
			r.mode = ModeLLM
		}
	}
	if err := r.survey(ctx); err != nil {
		return Result{}, err
	}
	if r.mode == ModePlanner {
		err = r.plan(ctx)
	} else {
		err = r.converse(ctx)
	}
	if err != nil {
		return Result{}, err
	}
	out := map[string]any{"actions": r.actions, "questions": r.asked, "skipped": r.skipped, "states": r.views(nil)}
	if err := r.log(ctx, "прогон завершён", "", nil, out); err != nil {
		return Result{}, err
	}
	return Result{SessionID: r.session, Mode: r.mode, Steps: r.step, Actions: r.actions, Questions: r.asked, Skipped: r.skipped}, nil
}

func (r *run) maxSteps() int {
	if r.a.MaxSteps > 0 {
		return r.a.MaxSteps
	}
	return MaxSteps
}

func (r *run) log(ctx context.Context, decision, tool string, in, out map[string]any) error {
	r.step++
	if in == nil {
		in = map[string]any{}
	}
	if out == nil {
		out = map[string]any{}
	}
	return r.a.Store.AppendTrace(ctx, TraceStep{
		Agent: AgentName, SessionID: r.session, HotelID: r.h.ID, Mode: r.mode, Step: r.step,
		Decision: decision, Tool: tool, Input: in, Output: out,
	})
}

func (r *run) refresh(ctx context.Context) error {
	facts, err := r.a.Store.HotelFacts(ctx, r.h.ID)
	if err != nil {
		return err
	}
	r.states = map[string]State{}
	for _, c := range r.a.Registry.All() {
		r.states[c.Key] = Assess(c, r.h, facts)
	}
	return nil
}

// views — состояния характеристик в порядке реестра; keys == nil — все.
func (r *run) views(keys map[string]bool) []map[string]any {
	var out []map[string]any
	for _, c := range r.a.Registry.All() {
		if keys == nil || keys[c.Key] {
			out = append(out, r.states[c.Key].View())
		}
	}
	return out
}

// survey — первый шаг: посмотреть факты отеля и записать, что свежее и пропускается и почему.
func (r *run) survey(ctx context.Context) error {
	if err := r.refresh(ctx); err != nil {
		return err
	}
	if err := r.log(ctx, "смотрю факты отеля перед работой", "get_hotel_facts",
		map[string]any{"hotel": r.h.Name}, map[string]any{"characteristics": r.views(nil)}); err != nil {
		return err
	}
	for _, c := range r.a.Registry.All() {
		if st := r.states[c.Key]; !st.Needs() {
			r.skipped++
			if err := r.log(ctx, fmt.Sprintf("пропускаю %s: %s", c.Key, st.Reason), "",
				map[string]any{"characteristic": c.Key}, map[string]any{"skipped": true}); err != nil {
				return err
			}
		}
	}
	return nil
}

// reject пишет отказ в журнал: попытка нарушить правило агента не пропадает.
func (r *run) reject(ctx context.Context, tool string, in map[string]any, why string) map[string]any {
	out := map[string]any{"error": why}
	_ = r.log(ctx, "отказ: "+why, tool, in, out)
	return out
}

func toolSig(tool string, params map[string]float64) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(tool)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%g", k, params[k])
	}
	return b.String()
}

// limitMarker — признак отказ из-за лимита шагов.
const limitMarker = "лимит шагов"

// opRunTool запускает инструмент стратегии idx характеристики key, если характеристика требует работы.
func (r *run) opRunTool(ctx context.Context, key string, idx int, params map[string]float64, why string) map[string]any {
	in := map[string]any{"characteristic": key, "strategy_index": idx}
	if len(params) > 0 {
		in["params"] = params
	}
	c, ok := r.a.Registry.Get(key)
	if !ok {
		return r.reject(ctx, "run_tool", in, "нет такой характеристики: "+key)
	}
	if st := r.states[key]; !st.Needs() {
		return r.reject(ctx, "run_tool", in, fmt.Sprintf("%s: %s, пересчёт не нужен", key, st.Reason))
	}
	if idx < 0 || idx >= len(c.Strategies) {
		return r.reject(ctx, "run_tool", in, fmt.Sprintf("у %s нет стратегии %d (всего %d)", key, idx, len(c.Strategies)))
	}
	tool := c.Strategies[idx].Tool
	sig := toolSig(tool, params)
	if r.ran[sig] {
		return r.reject(ctx, "run_tool", in, fmt.Sprintf("инструмент %s уже запускался в этом прогоне", tool))
	}
	if r.actions >= r.maxSteps() {
		return r.reject(ctx, "run_tool", in, fmt.Sprintf("%s %d исчерпан", limitMarker, r.maxSteps()))
	}
	r.actions++
	r.ran[sig] = true
	in["tool"] = tool
	summary, err := r.a.Runner.Run(ctx, r.h, tool, params)
	if err != nil {
		out := map[string]any{"error": err.Error()}
		_ = r.log(ctx, why, "run_tool", in, out)
		return out
	}
	if err := r.refresh(ctx); err != nil {
		return map[string]any{"error": err.Error()}
	}
	same := map[string]bool{}
	for _, o := range r.a.Registry.All() {
		for _, s := range o.Strategies {
			if s.Tool == tool {
				same[o.Key] = true
			}
		}
	}
	out := map[string]any{"summary": summary, "states": r.views(same)}
	_ = r.log(ctx, why, "run_tool", in, out)
	return out
}

// opRecompute просит движок правды пересобрать факт характеристики из активных свидетельств.
func (r *run) opRecompute(ctx context.Context, key, why string) map[string]any {
	in := map[string]any{"characteristic": key}
	if _, ok := r.a.Registry.Get(key); !ok {
		return r.reject(ctx, "recompute_fact", in, "нет такой характеристики: "+key)
	}
	if r.actions >= r.maxSteps() {
		return r.reject(ctx, "recompute_fact", in, fmt.Sprintf("%s %d исчерпан", limitMarker, r.maxSteps()))
	}
	r.actions++
	if err := r.a.Store.RecomputeFact(ctx, r.h.ID, r.h.District, key); err != nil {
		out := map[string]any{"error": err.Error()}
		_ = r.log(ctx, why, "recompute_fact", in, out)
		return out
	}
	if err := r.refresh(ctx); err != nil {
		return map[string]any{"error": err.Error()}
	}
	out := map[string]any{"states": r.views(map[string]bool{key: true})}
	_ = r.log(ctx, why, "recompute_fact", in, out)
	return out
}

// opAsk создаёт вопрос отельеру, только если выполнено правило: у характеристики есть ask_hotelier,
// уверенности нет или она ниже AskConfidence; не больше MaxQuestions на отель за прогон.
func (r *run) opAsk(ctx context.Context, key, why string) map[string]any {
	in := map[string]any{"characteristic": key}
	c, ok := r.a.Registry.Get(key)
	if !ok {
		return r.reject(ctx, "ask_hotelier", in, "нет такой характеристики: "+key)
	}
	st := r.states[key]
	reason := AskReason(c, st)
	switch {
	case c.AskHotelier == "":
		return r.reject(ctx, "ask_hotelier", in, key+": у характеристики нет вопроса отельеру")
	case reason == "":
		return r.reject(ctx, "ask_hotelier", in, fmt.Sprintf("%s: вопрос не нужен, уверенность не ниже %.2f", key, AskConfidence))
	case r.asked >= MaxQuestions:
		return r.reject(ctx, "ask_hotelier", in, fmt.Sprintf("лимит вопросов на отель за прогон: %d", MaxQuestions))
	}
	score := ValueScore(st)
	created, err := r.a.Store.CreateQuestion(ctx, Question{HotelID: r.h.ID, Key: key, Text: c.AskHotelier, Reason: reason, ValueScore: score})
	if err != nil {
		out := map[string]any{"error": err.Error()}
		_ = r.log(ctx, why, "ask_hotelier", in, out)
		return out
	}
	if created {
		r.asked++
	}
	out := map[string]any{"created": created, "question": c.AskHotelier, "reason": reason, "value_score": score}
	if !created {
		out["note"] = "такой открытый вопрос уже есть"
	}
	_ = r.log(ctx, why, "ask_hotelier", in, out)
	return out
}

// plan — детерминированный планировщик: по характеристикам реестра по порядку, по стратегиям по порядку.
func (r *run) plan(ctx context.Context) error {
	for _, c := range r.a.Registry.All() {
		for i, s := range c.Strategies {
			if !r.states[c.Key].Needs() {
				break
			}
			if r.ran[toolSig(s.Tool, nil)] {
				continue
			}
			st := r.states[c.Key]
			why := fmt.Sprintf("%s: %s; запускаю стратегию %d (%s) по порядку реестра", c.Key, st.Reason, i, s.Tool)
			if res := r.opRunTool(ctx, c.Key, i, nil, why); strings.Contains(fmt.Sprint(res["error"]), limitMarker) {
				return nil
			}
		}
		st := r.states[c.Key]
		if !st.Needs() || c.AskHotelier == "" {
			continue
		}
		if reason := AskReason(c, st); reason == "" {
			if err := r.log(ctx, fmt.Sprintf("%s: %s, но уверенность не ниже %.2f, отельера не спрашиваю", c.Key, st.Reason, AskConfidence),
				"", map[string]any{"characteristic": c.Key}, nil); err != nil {
				return err
			}
		} else if r.asked < MaxQuestions {
			r.opAsk(ctx, c.Key, fmt.Sprintf("%s: после расчёта %s; спрашиваю отельера", c.Key, reason))
		}
	}
	return nil
}

// converse — путь через LLM с tool calling.
func (r *run) converse(ctx context.Context) error {
	intro, _ := json.Marshal(r.views(nil))
	msgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Отель: «%s», район %s. Состояние характеристик (JSON):\n%s\nХарактеристики со статусом ok не трогай. Остальные доведи до результата.", r.h.Name, r.h.District, intro)},
	}
	tools := toolDefs()
	for round := 0; round < r.maxSteps(); round++ {
		reply, err := r.a.Chat.Chat(ctx, msgs, tools)
		if err != nil {
			return r.fallback(ctx, err)
		}
		msgs = append(msgs, reply)
		if len(reply.ToolCalls) == 0 {
			return r.log(ctx, "итог агента: "+strings.TrimSpace(reply.Content), "", nil, nil)
		}
		for _, call := range reply.ToolCalls {
			res := r.dispatch(ctx, call)
			b, _ := json.Marshal(res)
			msgs = append(msgs, Message{Role: "tool", ToolCallID: call.ID, Content: string(b)})
		}
	}
	return r.log(ctx, fmt.Sprintf("достигнут лимит шагов %d, останавливаюсь", r.maxSteps()), "", nil, nil)
}

// NotCachedError — признак ошибки режима кеша: на этот шаг нет сохранённого ответа.
type NotCachedError interface{ NotCached() bool }

// fallback — LLM недоступна или в кеше нет ответа: дальше работает планировщик, и это видно в журнале.
func (r *run) fallback(ctx context.Context, cause error) error {
	var miss NotCachedError
	why := cause.Error()
	if errors.As(cause, &miss) {
		why = "в кеше LLM нет ответа на этот шаг"
	}
	if err := r.log(ctx, "LLM недоступна: "+why+"; дальше работает детерминированный планировщик", "", nil, map[string]any{"error": cause.Error()}); err != nil {
		return err
	}
	r.mode = ModePlanner
	return r.plan(ctx)
}

type callArgs struct {
	Reason         string             `json:"reason"`
	Characteristic string             `json:"characteristic"`
	StrategyIndex  int                `json:"strategy_index"`
	Params         map[string]float64 `json:"params"`
}

func (r *run) dispatch(ctx context.Context, call ToolCall) map[string]any {
	var args callArgs
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return r.reject(ctx, call.Name, map[string]any{"arguments": call.Arguments}, "аргументы не JSON: "+err.Error())
	}
	why := strings.TrimSpace(args.Reason)
	if why == "" {
		why = "(модель не дала обоснования)"
	}
	switch call.Name {
	case "list_characteristics":
		out := map[string]any{"characteristics": r.characteristicsView()}
		_ = r.log(ctx, why, call.Name, nil, map[string]any{"count": len(r.a.Registry.All())})
		return out
	case "get_hotel_facts":
		var keys map[string]bool
		if args.Characteristic != "" {
			keys = map[string]bool{args.Characteristic: true}
		}
		out := map[string]any{"characteristics": r.views(keys)}
		_ = r.log(ctx, why, call.Name, map[string]any{"characteristic": args.Characteristic}, out)
		return out
	case "run_tool":
		return r.opRunTool(ctx, args.Characteristic, args.StrategyIndex, args.Params, why)
	case "recompute_fact":
		return r.opRecompute(ctx, args.Characteristic, why)
	case "ask_hotelier":
		return r.opAsk(ctx, args.Characteristic, why)
	}
	return r.reject(ctx, call.Name, nil, "неизвестный инструмент "+call.Name)
}

func (r *run) characteristicsView() []map[string]any {
	var out []map[string]any
	for _, c := range r.a.Registry.All() {
		var ss []map[string]any
		for i, s := range c.Strategies {
			m := map[string]any{"index": i, "tool": s.Tool}
			if s.Variant != "" {
				m["variant"] = s.Variant
			}
			ss = append(ss, m)
		}
		out = append(out, map[string]any{
			"key": c.Key, "title": c.Title, "level": c.Level, "origin": c.Origin,
			"strategies": ss, "has_hotelier_question": c.AskHotelier != "", "status": r.states[c.Key].Status,
		})
	}
	return out
}

const systemPrompt = `Ты агент-сборщик характеристик отеля. Ты не знаешь фактов и не записываешь их: значения считают инструменты, а уверенность и конфликты определяет движок правды. Ты решаешь только, что посчитать и когда спросить отельера.
Правила:
- Характеристики со статусом ok (свежо и уверенно) не трогай.
- Для missing, stale, disputed, weak и uncertain выбери стратегию из реестра (list_characteristics) и запусти инструмент run_tool. Один инструмент не запускай дважды.
- После расчёта смотри на результат. Если факт появился и уверенность не ниже 0,6, отельера не спрашивай.
- Вопрос отельеру (ask_hotelier) разрешён, только если у характеристики есть вопрос, а после расчёта данных нет или уверенность ниже 0,6. Не больше трёх вопросов.
- В каждом вызове в поле reason пиши одну короткую фразу с обоснованием решения по-русски.
- Когда работа закончена, ответь коротким итогом без вызова инструментов.`

func toolDefs() []ToolDef {
	reason := `"reason":{"type":"string","description":"краткое обоснование решения"}`
	key := `"characteristic":{"type":"string","description":"ключ характеристики из реестра"}`
	obj := func(props, required string) json.RawMessage {
		return json.RawMessage(`{"type":"object","properties":{` + props + `},"required":[` + required + `]}`)
	}
	return []ToolDef{
		{Name: "list_characteristics", Description: "Список характеристик реестра со стратегиями (индекс, инструмент) и текущим статусом у этого отеля.", Parameters: obj(reason, `"reason"`)},
		{Name: "get_hotel_facts", Description: "Факты отеля: статус, значение, уверенность, устаревание, конфликты. Без characteristic — по всем.", Parameters: obj(reason+","+key, `"reason"`)},
		{Name: "run_tool", Description: "Запустить инструмент стратегии характеристики по этому отелю. Инструмент пишет свидетельства, факт пересобирает движок правды. params — числовые переопределения параметров инструмента (необязательно).",
			Parameters: obj(reason+","+key+`,"strategy_index":{"type":"integer","description":"индекс стратегии из list_characteristics, по умолчанию 0"},"params":{"type":"object","additionalProperties":{"type":"number"}}`, `"reason","characteristic"`)},
		{Name: "recompute_fact", Description: "Пересчитать факт характеристики движком правды из имеющихся свидетельств, без запуска инструмента.", Parameters: obj(reason+","+key, `"reason","characteristic"`)},
		{Name: "ask_hotelier", Description: "Создать вопрос отельеру по характеристике. Разрешено, только если после расчёта данных нет или уверенность ниже 0,6.", Parameters: obj(reason+","+key, `"reason","characteristic"`)},
	}
}

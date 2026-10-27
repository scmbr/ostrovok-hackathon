package dialog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// Агент диалога с tool calling. Модель выбирает, какие инструменты вызвать и в каком порядке, и пересказывает
// готовый результат; факты она не пишет — таких инструментов нет. Что именно искать, спрашивать ли гостя и можно ли
// отдать текст, решает код: уточнение проходит то же правило, что и без модели, поиск считает search.Service,
// текст проверяется на числа и названия отелей (VerifyRephrase).

const systemPrompt = `Ты — агент диалога сервиса бронирования отелей Барселоны. Гость пишет своими словами, что ему важно в отеле.
Ты НЕ источник фактов: все числа, названия отелей и свойства берутся только из результатов инструментов.

Порядок работы:
1. Вызови parse_baseline с текстом гостя. Он разбирает фразу по реестру характеристик.
2. Если в ответе есть ambiguities или weak_importance, вызови ask_clarification для каждой такой фразы. Решение, задавать ли вопрос гостю, принимает система; ты его не обходишь. Если система задала вопрос, жди ответа гостя и ничего не пиши.
3. Если гость назвал то, чего нет в реестре (unmet), вызови log_unmet_wish: text — формулировка гостя, normalized — короткая каноническая форма на русском (например «морской воздух»), proposal — по возможности {"key","title","how"} с предложением новой характеристики.
4. Если гость просит «похожий на …», вызови similar. Иначе вызови search; если разбор что-то упустил, добавь желания через аргумент wishes (только ключи из list_characteristics), «без X» — через remove.
5. Ответь гостю короткой вводной: 1–2 предложения одним абзацем, без списков и разметки. Скажи, что понято, и назови лучший отель (его название — как в результате search или similar). Строки по каждому отелю (статусы, значения, источники) система добавит сама: не пересказывай их и не перечисляй свойства отелей. Используй только названия отелей и числа из результата. Не оценивай отели («соответствует требованиям», «идеально», «отлично») и не используй слова уверенности («точно», «наверняка»); «гарантирован» и «обеспечен» допустимы, только если это прямо сказано в результате для этого отеля.`

var toolDefs = []agent.ToolDef{
	{Name: "list_characteristics", Description: "Список характеристик реестра: ключ, название, тип значения, допустимые значения.",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
	{Name: "parse_baseline", Description: "Детерминированный разбор фразы гостя по реестру: желания, важность, числа, «без X», неоднозначные фразы, непонятое. Понятое сразу вносится в запрос сессии.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"фраза гостя"}},"required":["text"]}`)},
	{Name: "search", Description: "Поиск отелей по накопленным желаниям сессии. Перед поиском система сама применяет правило уточнения. Возвращает лучших и explanation_template.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"wishes":{"type":"array","description":"дополнительные желания, которые разбор упустил","items":{"type":"object","properties":{"key":{"type":"string"},"op":{"type":"string","enum":["eq","le","ge"]},"value":{},"importance":{"type":"string","enum":["must","prefer"]}},"required":["key"]}},` +
			`"remove":{"type":"array","items":{"type":"string"},"description":"ключи желаний, которые гость отменил"},` +
			`"price_max":{"type":"integer"},"stars_min":{"type":"integer"}}}`)},
	{Name: "similar", Description: "Отели с профилем фактов, близким к названному (без звёзд и цены). Укажи один из аргументов.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"ordinal":{"type":"integer","description":"номер в последней выдаче"},"current":{"type":"boolean","description":"«этот» отель"},"name":{"type":"string"}}}`)},
	{Name: "get_hotel", Description: "Факты уровня отеля с уверенностью.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"hotel_id":{"type":"integer"}},"required":["hotel_id"]}`)},
	{Name: "ask_clarification", Description: "Запросить уточнение по неоднозначной фразе или мягкой важности. Система задаёт вопрос, только если от ответа зависит топ-3 выдачи и лимит не исчерпан; иначе вернёт принятое допущение.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"phrase":{"type":"string","description":"фраза из ambiguities или ключ из weak_importance"}},"required":["phrase"]}`)},
	{Name: "log_unmet_wish", Description: "Записать желание гостя, которого нет в реестре, в журнал спроса. Повторы считаются.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"normalized":{"type":"string"},"proposal":{"type":"object","properties":{"key":{"type":"string"},"title":{"type":"string"},"how":{"type":"string"}}}},"required":["text"]}`)},
}

// userPrompt — сообщение гостя и состояние сессии: то, чего модель не помнит между ходами.
func (r *run) userPrompt() string {
	var b strings.Builder
	if strings.TrimSpace(r.req.Message) == "" {
		fmt.Fprintf(&b, "Гость ответил на уточнение кнопкой; система уже применила ответ (%s). Новых слов нет: вызови search с пустыми аргументами {} и ответь по его explanation_template.\n", r.applied)
	} else {
		fmt.Fprintf(&b, "Сообщение гостя: «%s»\n", r.req.Message)
	}
	if len(r.st.Wishes) > 0 || r.st.Filters != (domain.Filters{}) {
		fmt.Fprintf(&b, "Уже накоплено в запросе: %s.\n", r.e.understood(domain.Query{Wishes: r.st.Wishes, Filters: r.st.Filters}))
	}
	if len(r.st.Last) > 0 {
		parts := make([]string, len(r.st.Last))
		for i, s := range r.st.Last {
			parts[i] = fmt.Sprintf("%d. %s (id %d)", i+1, s.Name, s.ID)
		}
		fmt.Fprintf(&b, "В прошлой выдаче: %s.\n", strings.Join(parts, "; "))
	}
	if r.st.Current != 0 {
		fmt.Fprintf(&b, "Открытый («этот») отель: id %d.\n", r.st.Current)
	}
	return b.String()
}

// turnLLM — ход с моделью. handled=false: модель недоступна или промах кеша — ход нужно повторить по правилам.
func (e *Engine) turnLLM(ctx context.Context, req Request, emit func(Event)) (bool, error) {
	if strings.TrimSpace(req.Message) == "" && req.Choice == "" {
		return false, nil
	}
	mode := e.ChatMode
	if mode == "" {
		mode = ModeLLM
	}
	r, err := e.newRun(ctx, req, mode, emit)
	if err != nil {
		return true, err
	}
	r.applyPending()
	msgs := []agent.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: r.userPrompt()}}

	var final string
	calls, rejected := 0, 0
loop:
	for calls < agent.MaxLLMCalls {
		reply, err := e.Chat.Chat(ctx, msgs, toolDefs)
		calls++
		if err != nil {
			reason := err.Error()
			var nc interface{ NotCached() bool }
			if errors.As(err, &nc) {
				reason = "в кеше нет ответа на этот шаг"
			}
			r.tr.step("модель недоступна — перехожу на правила", "fallback_rules", nil, map[string]any{"reason": reason})
			return false, nil
		}
		msgs = append(msgs, reply)
		if len(reply.ToolCalls) == 0 {
			final = reply.Content
			break
		}
		for _, tc := range reply.ToolCalls {
			out, err := r.callTool(tc)
			res := map[string]any{"ok": err == nil}
			content := out
			if err != nil {
				res["error"] = err.Error()
				content = `{"error":` + jsonString(err.Error()) + `}`
				rejected++
			} else {
				rejected = 0
				res["result"] = json.RawMessage(out)
			}
			r.tr.step("модель вызвала инструмент "+tc.Name, tc.Name, argsMap(tc.Arguments), res)
			msgs = append(msgs, agent.Message{Role: "tool", ToolCallID: tc.ID, Content: content})
			if r.ask != nil {
				break loop // система задала уточнение: ответа модели не ждём
			}
		}
		if rejected >= agent.MaxConsecutiveRejections {
			r.tr.step("слишком много отказов подряд — останавливаю модель", "stop", nil, map[string]any{"rejected": rejected})
			break
		}
	}
	if calls >= agent.MaxLLMCalls && final == "" && r.ask == nil {
		r.tr.step("лимит вызовов модели исчерпан", "stop", nil, map[string]any{"calls": calls})
	}

	// Страховка: правило уточнения и поиск обязательны, даже если модель их пропустила.
	if r.ask == nil {
		if !r.parsed {
			r.parse(req.Message)
		}
		if err := r.clarifyAll(); err != nil {
			return true, err
		}
	}
	if r.ask == nil && r.srch == nil && r.sim == nil {
		switch {
		case r.simRef != nil:
			if err := r.runSimilar(*r.simRef); err != nil && !errors.Is(err, errNoSimilarBase) {
				return true, err
			}
		case len(r.st.Wishes) > 0 || r.st.Filters != (domain.Filters{}):
			if err := r.runSearch(); err != nil {
				return true, err
			}
		}
	}
	return true, r.finish(final, emit)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func argsMap(args string) map[string]any {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return map[string]any{"raw": args}
	}
	return m
}

// callTool выполняет вызов инструмента. Ошибка возвращается модели и считается отказом.
func (r *run) callTool(tc agent.ToolCall) (string, error) {
	switch tc.Name {
	case "list_characteristics":
		return r.toolList()
	case "parse_baseline":
		var a struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(tc.Arguments), &a); err != nil || strings.TrimSpace(a.Text) == "" {
			return "", errors.New("нужен аргумент text")
		}
		return r.toolParse(a.Text)
	case "search":
		return r.toolSearch(tc.Arguments)
	case "similar":
		return r.toolSimilar(tc.Arguments)
	case "get_hotel":
		return r.toolHotel(tc.Arguments)
	case "ask_clarification":
		var a struct {
			Phrase string `json:"phrase"`
		}
		if err := json.Unmarshal([]byte(tc.Arguments), &a); err != nil || a.Phrase == "" {
			return "", errors.New("нужен аргумент phrase")
		}
		return r.toolAsk(a.Phrase)
	case "log_unmet_wish":
		return r.toolUnmet(tc.Arguments)
	}
	return "", fmt.Errorf("инструмента %q нет", tc.Name)
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func (r *run) searchableKeys() []string {
	var keys []string
	for _, c := range r.e.Registry.All() {
		if domain.Searchable(c) {
			keys = append(keys, c.Key)
		}
	}
	return keys
}

func (r *run) toolList() (string, error) {
	type item struct {
		Key         string   `json:"key"`
		Title       string   `json:"title"`
		ValueType   string   `json:"value_type"`
		Values      []string `json:"values,omitempty"`
		Description string   `json:"description,omitempty"`
	}
	var out []item
	for _, c := range r.e.Registry.All() {
		if domain.Searchable(c) {
			out = append(out, item{c.Key, c.Title, c.ValueType, c.Values, c.Description})
		}
	}
	return marshal(out)
}

func (r *run) toolParse(text string) (string, error) {
	p := r.parse(text)
	type amb struct {
		Phrase  string   `json:"phrase"`
		Options []string `json:"options"`
	}
	ambs := []amb{}
	for _, a := range p.Ambiguities {
		labels := make([]string, len(a.Options))
		for i, o := range a.Options {
			labels[i] = o.Label
		}
		ambs = append(ambs, amb{a.Phrase, labels})
	}
	wishes := []map[string]any{}
	for _, w := range p.Wishes {
		wishes = append(wishes, map[string]any{"key": w.Key, "value": w.Value, "importance": w.Importance})
	}
	out := map[string]any{
		"wishes": wishes, "removed": p.Remove, "filters": p.Filters, "ambiguities": ambs,
		"weak_importance": p.WeakKeys, "unmet": p.Unmet, "notes": p.Notes, "similar_requested": p.Similar != nil,
	}
	return marshal(out)
}

func (r *run) toolAsk(phrase string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(phrase))
	for i, a := range r.amb {
		if strings.ToLower(a.Phrase) != norm {
			continue
		}
		q, note, err := r.e.clarifyAmbiguity(r.ctx, &r.st, a, &r.clar, r.tr)
		if err != nil {
			return "", err
		}
		r.amb = append(r.amb[:i], r.amb[i+1:]...)
		return r.clarifyResult(q, note, defaultOption(a).Label)
	}
	for i, k := range r.weak {
		if k != norm && strings.ToLower(k) != norm {
			continue
		}
		q, note, err := r.e.clarifyImportance(r.ctx, &r.st, k, &r.clar, r.tr)
		if err != nil {
			return "", err
		}
		r.weak = append(r.weak[:i], r.weak[i+1:]...)
		return r.clarifyResult(q, note, "желательно")
	}
	return "", fmt.Errorf("фразы %q нет среди неразобранных: вызови parse_baseline и возьми фразу из ambiguities или weak_importance", phrase)
}

func (r *run) clarifyResult(q *Clarification, note, assumed string) (string, error) {
	if q != nil {
		r.ask = q
		return marshal(map[string]any{"asked": true, "question": q.Question, "next": "жди ответа гостя"})
	}
	return marshal(map[string]any{"asked": false, "assumed": assumed, "note": note})
}

func (r *run) toolSearch(args string) (string, error) {
	var a struct {
		Wishes   []domain.Desire `json:"wishes"`
		Remove   []string        `json:"remove"`
		PriceMax int             `json:"price_max"`
		StarsMin int             `json:"stars_min"`
	}
	if args != "" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", fmt.Errorf("аргументы search: %w", err)
		}
	}
	if !r.parsed {
		r.parse(r.req.Message)
	}
	if r.sim != nil && r.simRef != nil {
		return "", errors.New("гость просил похожие отели, и similar уже выполнен: ответь по его explanation_template, search не нужен")
	}
	// Сначала «убрать», потом «добавить»: желание, названное в обоих списках, остаётся.
	for _, k := range a.Remove {
		r.st.Merge(r.e.Registry, domain.ParseResult{Remove: []string{k}})
	}
	for _, d := range a.Wishes {
		nd, _, err := domain.NormalizeWish(r.e.Registry, d)
		if err != nil {
			return "", fmt.Errorf("желание отклонено: %w; допустимые ключи: %s", err, strings.Join(r.searchableKeys(), ", "))
		}
		r.st.Wishes = upsert(r.st.Wishes, r.st.withFloor(r.e.Registry, nd))
	}
	if a.PriceMax > 0 {
		r.st.Filters.PriceMax = a.PriceMax
	}
	if a.StarsMin > 0 {
		r.st.Filters.StarsMin = a.StarsMin
	}
	if err := r.clarifyAll(); err != nil {
		return "", err
	}
	if r.ask != nil {
		return marshal(map[string]any{"clarification_asked": true, "question": r.ask.Question, "next": "жди ответа гостя"})
	}
	if len(r.st.Wishes) == 0 && r.st.Filters == (domain.Filters{}) {
		return "", errors.New("в запросе нет желаний: сначала parse_baseline")
	}
	if err := r.runSearch(); err != nil {
		return "", err
	}
	type hotel struct {
		Rank     int      `json:"rank"`
		ID       int64    `json:"id"`
		Name     string   `json:"name"`
		Category string   `json:"category"`
		PriceEUR int      `json:"price_eur"`
		Lines    []string `json:"lines"`
		Notes    []string `json:"notes,omitempty"`
	}
	hs := []hotel{}
	for i, x := range r.srch.exps {
		if i >= topN {
			break
		}
		h := r.srch.res.Hotels[i]
		hs = append(hs, hotel{Rank: i + 1, ID: h.HotelID, Name: h.Name, Category: h.Best.Name, PriceEUR: h.Best.PriceEUR, Lines: x.Lines, Notes: x.Notes})
	}
	return marshal(map[string]any{
		"found": len(r.srch.res.Hotels), "hotels": hs,
		"explanation_template": r.e.resultsText(r.srch.res, r.srch.exps, r.notes, r.unmet),
	})
}

func (r *run) toolSimilar(args string) (string, error) {
	var a struct {
		Ordinal int    `json:"ordinal"`
		Current bool   `json:"current"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("аргументы similar: %w", err)
	}
	if a.Ordinal == 0 && !a.Current && a.Name == "" {
		return "", errors.New("укажи ordinal, current или name")
	}
	if !r.parsed {
		r.parse(r.req.Message)
	}
	if r.simRef == nil {
		return "", errors.New("гость не просил похожие отели (в его фразе нет «похожий на …»): вызови search")
	}
	// На кого ссылается гость, определяет разбор («второй» — второй в выдаче); аргументы модели не перебивают его.
	ref := *r.simRef
	if err := r.runSimilar(ref); err != nil {
		return "", err
	}
	type item struct {
		Rank       int    `json:"rank"`
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Difference string `json:"main_difference,omitempty"`
	}
	items := []item{}
	for i, s := range r.sim.list {
		if i >= topN {
			break
		}
		items = append(items, item{i + 1, s.ID, s.Name, r.e.mainDifferences(s)})
	}
	return marshal(map[string]any{
		"base": r.sim.base.Name, "similar": items,
		"explanation_template": r.e.similarText(r.sim.base, r.sim.list, r.notes, r.unmet),
	})
}

func (r *run) toolHotel(args string) (string, error) {
	var a struct {
		HotelID int64 `json:"hotel_id"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil || a.HotelID == 0 {
		return "", errors.New("нужен hotel_id")
	}
	card, err := r.e.Search.Hotel(r.ctx, a.HotelID)
	if err != nil {
		return "", err
	}
	type fact struct {
		Key        string  `json:"key"`
		Title      string  `json:"title"`
		Value      any     `json:"value"`
		Confidence float64 `json:"confidence"`
	}
	facts := []fact{}
	for _, f := range card.Facts {
		if f.Fact.EntityType != domain.EntityHotel {
			continue
		}
		c, ok := r.e.Registry.Get(f.Fact.Key)
		if !ok {
			continue
		}
		facts = append(facts, fact{c.Key, c.Title, f.Fact.Value[c.FieldName()], f.Fact.Confidence})
	}
	return marshal(map[string]any{"id": card.ID, "name": card.Name, "district": card.District, "facts": facts})
}

func (r *run) toolUnmet(args string) (string, error) {
	var a struct {
		Text       string          `json:"text"`
		Normalized string          `json:"normalized"`
		Proposal   json.RawMessage `json:"proposal"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Text) == "" {
		return "", errors.New("нужен аргумент text")
	}
	// Журнал спроса — для желаний вне реестра: то, что парсер понимает, туда не пишем.
	if p := r.e.Lexicon.Parse(a.Text); len(p.Wishes) > 0 || len(p.Ambiguities) > 0 {
		return "", fmt.Errorf("«%s» уже понятно по реестру (%s), в журнал не пишу", a.Text, strings.Join(wishKeys(p.Wishes), ", "))
	}
	var proposal []byte
	if len(a.Proposal) > 0 && string(a.Proposal) != "null" {
		proposal = a.Proposal
	}
	r.queueUnmet(a.Text, strings.ToLower(strings.TrimSpace(a.Normalized)), proposal)
	return marshal(map[string]any{"logged": true})
}

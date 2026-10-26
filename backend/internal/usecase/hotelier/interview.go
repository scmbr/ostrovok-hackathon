package hotelier

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// Interviewer выбирает вопросы отельеру по ценности и бюджету и обрабатывает ответы.
type Interviewer struct {
	Registry domain.Registry
	Store    Store
	Impact   Impact // nil — влияние на категории не считается
	LLM      LLM    // nil — шаблоны вопросов и разбор да / нет
	Now      func() time.Time
}

// Options — параметры раунда.
type Options struct {
	// Answer — имитированная персона сразу отвечает на отправленные вопросы. Без него вопросы остаются в дайджесте.
	Answer bool
}

// Outcome — результат обработки одного ответа.
type Outcome struct {
	Question Question
	Parsed   Parsed
	Status   string
}

// Result — итог раунда по отелю.
type Result struct {
	Hotel      Hotel
	Candidates []Question // все кандидаты раунда после записи, по убыванию ценности
	Sent       []Question
	Answers    []Outcome
	// Waiting — прошли порог, но не уместились в бюджет; LowValue — ниже порога.
	Waiting, LowValue int
	// BudgetLeft — сколько вопросов можно было отправить в начале раунда.
	BudgetLeft int
}

func (i *Interviewer) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

type session struct {
	id   string
	step int
}

func (i *Interviewer) trace(ctx context.Context, s *session, h Hotel, mode, decision, tool string, in, out map[string]any) {
	s.step++
	_ = i.Store.AppendTrace(ctx, agent.TraceStep{
		Agent: AgentName, SessionID: s.id, HotelID: h.ID, Mode: mode, Step: s.step,
		Decision: decision, Tool: tool, Input: in, Output: out,
	})
}

// Round — один раунд интервьюера по отелю: кандидаты, ценность, бюджет, отправка и, если нужно, ответы персоны.
func (i *Interviewer) Round(ctx context.Context, hotelID int64, opt Options) (Result, error) {
	var res Result
	h, err := i.Store.Hotel(ctx, hotelID)
	if err != nil {
		return res, err
	}
	res.Hotel = h
	facts, err := i.Store.HotelFacts(ctx, hotelID)
	if err != nil {
		return res, fmt.Errorf("факты отеля: %w", err)
	}
	demand, err := i.Store.Demand(ctx, h.RegionID)
	if err != nil {
		return res, fmt.Errorf("спрос: %w", err)
	}
	maxDemand := 0
	for _, n := range demand {
		maxDemand = max(maxDemand, n)
	}
	existing, err := i.Store.Questions(ctx, hotelID)
	if err != nil {
		return res, fmt.Errorf("вопросы отеля: %w", err)
	}
	trust, err := i.Store.Trust(ctx, domain.HotelierAuthor(hotelID))
	if err != nil {
		return res, fmt.Errorf("репутация: %w", err)
	}
	s := &session{id: fmt.Sprintf("hotelier-%d-%d", hotelID, i.now().UnixNano())}
	info := agent.HotelInfo{ID: h.ID, Name: h.Name, District: h.District}

	for _, c := range i.Registry.All() {
		if c.AskHotelier == "" {
			continue
		}
		ex := latest(existing, c.Key)
		if ex != nil && (ex.Status == StatusOpen || ex.Status == StatusNeedsClarification || ex.Status == StatusAnswered) {
			continue
		}
		st := agent.Assess(c, info, facts)
		reason := agent.AskReason(c, st)
		if reason == "" && (st.Status == agent.StatusDisputed || st.Status == agent.StatusWeak) {
			reason = st.Reason
		}
		if reason == "" {
			if waiting(ex) {
				_ = i.Store.Close(ctx, ex.ID, "данные появились: "+st.Reason)
				i.trace(ctx, s, h, agent.ModePlanner, "вопрос закрыт: "+st.Reason, "", map[string]any{"characteristic": c.Key}, nil)
			}
			continue
		}
		q := i.score(ctx, h, c, st, facts, demand, maxDemand, trust)
		q.Reason = i.reasonText(c, st, q.Score)
		if waiting(ex) {
			q.ID = ex.ID
		}
		saved, err := i.Store.UpsertCandidate(ctx, q)
		if err != nil {
			return res, fmt.Errorf("%s: %w", c.Key, err)
		}
		res.Candidates = append(res.Candidates, saved)
		i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("кандидат %s: ценность %.2f (%s)", saved.Key, saved.Score.Value, saved.Status), "score",
			map[string]any{"characteristic": c.Key, "reason": saved.Reason}, map[string]any{"score": saved.Score, "status": saved.Status, "format": saved.Format})
	}
	sort.SliceStable(res.Candidates, func(a, b int) bool { return res.Candidates[a].Score.Value > res.Candidates[b].Score.Value })

	sentRecent, err := i.Store.SentSince(ctx, hotelID, i.now().AddDate(0, 0, -domain.QuestionBudgetDays))
	if err != nil {
		return res, fmt.Errorf("бюджет: %w", err)
	}
	res.BudgetLeft = max(0, domain.QuestionBudget-sentRecent)
	left := res.BudgetLeft
	for k := range res.Candidates {
		q := &res.Candidates[k]
		switch {
		case q.Status == StatusLowValue:
			res.LowValue++
		case left == 0:
			res.Waiting++
			i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("%s ждёт: бюджет %d вопросов за %d дней исчерпан", q.Key, domain.QuestionBudget, domain.QuestionBudgetDays), "budget",
				map[string]any{"characteristic": q.Key}, map[string]any{"sent_recent": sentRecent})
		default:
			left--
			c, _ := i.Registry.Get(q.Key)
			text, mode := i.phrase(ctx, h, c, hotelFactValue(facts, c.Key, h.ID), q.Format)
			at := i.now()
			if err := i.Store.Send(ctx, q.ID, text, q.Format, h.Persona, at); err != nil {
				return res, fmt.Errorf("отправка %s: %w", q.Key, err)
			}
			q.Text, q.Status, q.Persona, q.SentAt = text, StatusOpen, h.Persona, &at
			res.Sent = append(res.Sent, *q)
			i.trace(ctx, s, h, mode, fmt.Sprintf("отправлен вопрос по %s (формат %s)", q.Key, q.Format), "ask_hotelier",
				map[string]any{"characteristic": q.Key, "format": q.Format}, map[string]any{"question": text, "value": q.Score.Value})
		}
	}

	if opt.Answer {
		for _, q := range res.Sent {
			out, err := i.simulate(ctx, s, h, q)
			if err != nil {
				return res, err
			}
			if out != nil {
				res.Answers = append(res.Answers, *out)
			}
		}
	}
	return res, nil
}

// latest — последний по id вопрос по характеристике.
func latest(qs []Question, key string) *Question {
	var out *Question
	for k := range qs {
		if qs[k].Key == key && (out == nil || qs[k].ID > out.ID) {
			out = &qs[k]
		}
	}
	return out
}

func hotelFact(facts []domain.Fact, key string, hotelID int64) *domain.Fact {
	for k := range facts {
		f := &facts[k]
		if f.Key == key && f.EntityType == domain.EntityHotel && f.EntityID == strconv.FormatInt(hotelID, 10) {
			return f
		}
	}
	return nil
}

func hotelFactValue(facts []domain.Fact, key string, hotelID int64) map[string]any {
	if f := hotelFact(facts, key, hotelID); f != nil {
		return f.Value
	}
	return nil
}

// score считает ценность вопроса: спрос × неуверенность × влияние.
func (i *Interviewer) score(ctx context.Context, h Hotel, c domain.Characteristic, st agent.State, facts []domain.Fact,
	demand map[string]int, maxDemand int, trust float64) Question {
	primary := hotelFact(facts, c.Key, h.ID)
	has := primary != nil && st.Confidence != nil
	conf, valueStr := 0.0, ""
	if has {
		conf = primary.Confidence
		valueStr = fmt.Sprint(primary.Value[c.FieldName()])
	}
	answers := allowedValues(c)
	k := max(len(answers), 2)
	cur := domain.FactState{Value: valueStr, Confidence: conf}
	factImpact := 0.0
	if !has {
		factImpact = 1
	}
	for _, a := range answers {
		if factImpact == 1 {
			break
		}
		r := domain.HotelierAnswerReliability(trust, slicesContains(c.FavorableValues, a))
		factImpact = max(factImpact, domain.FactImpact(true, cur, []string{a}, r))
	}
	changed, total := 0, 0
	if i.Impact != nil {
		if ch, tot, err := i.Impact.CategoryImpact(ctx, h.ID, c.Key); err == nil {
			changed, total = ch, tot
		}
	}
	sc := Score{Demand: demand[c.Key], DemandNorm: domain.NormalizedDemand(demand[c.Key], maxDemand), CategoriesChanged: changed, Categories: total}
	sc.Uncertainty = domain.Uncertainty(has, conf, k, st.Status == agent.StatusDisputed, st.Status == agent.StatusWeak)
	sc.Impact = domain.Impact(factImpact, changed, total)
	sc.Value = round2(domain.QuestionValue(sc.DemandNorm, sc.Uncertainty, sc.Impact))
	q := Question{HotelID: h.ID, Key: c.Key, Text: c.AskHotelier, Score: sc, Format: domain.FormatOpen, Status: StatusPending}
	if has {
		q.Format = domain.FormatConfirm
	}
	if sc.Value < domain.QuestionSendThreshold {
		q.Status = StatusLowValue
	}
	return q
}

func slicesContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

// reasonText — причина вопроса для дайджеста отельера: спрос, неуверенность, влияние.
func (i *Interviewer) reasonText(c domain.Characteristic, st agent.State, sc Score) string {
	var parts []string
	if sc.Demand > 0 {
		parts = append(parts, guestsSearched(sc.Demand, c.Title))
	}
	switch {
	case st.Confidence == nil:
		parts = append(parts, "данных нет")
	default:
		parts = append(parts, "уверенность "+strings.Replace(strconv.FormatFloat(*st.Confidence, 'f', 2, 64), ".", ",", 1))
	}
	switch st.Status {
	case agent.StatusDisputed:
		parts = append(parts, "источники спорят")
	case agent.StatusWeak:
		parts = append(parts, "источники слегка расходятся")
	}
	switch {
	case sc.Categories > 0 && sc.CategoriesChanged > 0:
		parts = append(parts, fmt.Sprintf("ответ изменит вероятность в категориях: %d из %d", sc.CategoriesChanged, sc.Categories))
	case st.Confidence == nil:
		parts = append(parts, "ответ заполнит пробел")
	case sc.Impact >= 1:
		parts = append(parts, "ответ изменит значение или уверенность")
	}
	return strings.Join(parts, "; ")
}

// guestsSearched — «14 гостей искали «Тишина ночью»» с согласованием по числу.
func guestsSearched(n int, title string) string {
	word, verb := "гостей", "искали"
	switch {
	case n%10 == 1 && n%100 != 11:
		word, verb = "гость", "искал"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = "гостя"
	}
	return fmt.Sprintf("%d %s %s «%s»", n, word, verb, title)
}

// simulate: имитированная персона отвечает на отправленный вопрос. У живого отельера (персоны нет) ответа ждёт кабинет.
func (i *Interviewer) simulate(ctx context.Context, s *session, h Hotel, q Question) (*Outcome, error) {
	if h.Persona == "" {
		return nil, nil
	}
	truth, _, err := i.Store.Truth(ctx, h.ID, q.Key)
	if err != nil {
		return nil, fmt.Errorf("истина мока: %w", err)
	}
	text := domain.PersonaAnswer(h.Persona, domain.AnswerRequest{HotelID: h.ID, Key: q.Key, Format: q.Format, Truth: truth})
	i.trace(ctx, s, h, agent.ModePlanner, "персона "+h.Persona+" ответила на "+q.Key, "persona_answer",
		map[string]any{"characteristic": q.Key}, map[string]any{"answer": text})
	return i.processAnswer(ctx, s, h, q, text)
}

// Answer обрабатывает ответ отельера из кабинета на отправленный вопрос.
func (i *Interviewer) Answer(ctx context.Context, q Question, text string) (*Outcome, error) {
	h, err := i.Store.Hotel(ctx, q.HotelID)
	if err != nil {
		return nil, err
	}
	s := &session{id: fmt.Sprintf("hotelier-%d-%d", q.HotelID, i.now().UnixNano())}
	return i.processAnswer(ctx, s, h, q, text)
}

// processAnswer разбирает ответ, пишет свидетельство отельера (если значение найдено) и обновляет вопрос.
// Если разбор неуверен, вопрос получает needs_clarification, значение не выдумывается.
func (i *Interviewer) processAnswer(ctx context.Context, s *session, h Hotel, q Question, text string) (*Outcome, error) {
	c, ok := i.Registry.Get(q.Key)
	if !ok {
		return nil, fmt.Errorf("нет характеристики %q", q.Key)
	}
	facts, err := i.Store.HotelFacts(ctx, h.ID)
	if err != nil {
		return nil, err
	}
	q.AnswerText = text
	parsed := i.Parse(ctx, c, q, hotelFactValue(facts, c.Key, h.ID))
	now := i.now()
	status := StatusNeedsClarification
	parsedJSON := map[string]any{"found": parsed.Found, "note": parsed.Note, "parsed_by": parsed.Mode}
	if parsed.Found {
		status = StatusAnswered
		parsedJSON["claimed"] = parsed.Claimed
		until := now.Add(c.TTL())
		ev := domain.Evidence{
			EntityType: domain.EntityHotel, EntityID: strconv.FormatInt(h.ID, 10), Key: c.Key, Claimed: parsed.Claimed,
			Source: domain.SourceHotelier, Method: HotelierMethod, AuthorType: domain.AuthorHotelier, AuthorID: strconv.FormatInt(h.ID, 10),
			Reliability: domain.HotelierBaseReliability, ObservedAt: now, ValidUntil: &until,
			Details: map[string]any{"question_id": q.ID, "answer_text": text, "format": q.Format},
		}
		if err := i.Store.SaveAnswerEvidence(ctx, ev); err != nil {
			return nil, fmt.Errorf("свидетельство отельера: %w", err)
		}
	}
	if err := i.Store.RecordAnswer(ctx, q.ID, status, text, parsedJSON, now); err != nil {
		return nil, err
	}
	q.Status, q.Parsed = status, parsedJSON
	i.trace(ctx, s, h, parsed.Mode, fmt.Sprintf("ответ по %s разобран: %s", q.Key, status), "parse_answer",
		map[string]any{"characteristic": q.Key, "answer": text}, parsedJSON)
	return &Outcome{Question: q, Parsed: parsed, Status: status}, nil
}

// HotelierMethod — метод свидетельства отельера, полученного из ответа на вопрос.
const HotelierMethod = "hotelier_answer@1"

// waiting — копящийся вопрос (ещё не отправлен): его можно обновить или закрыть.
func waiting(q *Question) bool {
	return q != nil && (q.Status == StatusPending || q.Status == StatusLowValue)
}

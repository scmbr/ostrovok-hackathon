package hotelier

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/reputation"
)

// Ошибки кабинета отельера (HTTP: 404 и 409).
var (
	ErrNotFound = errors.New("не найдено")
	ErrState    = errors.New("вопрос не ждёт ответа")
	ErrInvalid  = errors.New("недопустимое значение")
)

// InboxItem — вопрос в дайджесте отельера.
type InboxItem struct {
	Question Question
	Title    string // название характеристики
	// ReasonKind — почему спрашиваем: no_data, low_confidence, conflict (расчёт спорит с отелем) или guests (гости сообщили).
	ReasonKind string
	// Reason — причина по текущему состоянию, если сохранённая в вопросе устарела (спор снят); пусто — читать Question.Reason.
	Reason string
}

// Виды причин вопроса в дайджесте (InboxItem.ReasonKind).
const (
	ReasonNoData        = "no_data"
	ReasonLowConfidence = "low_confidence"
	ReasonConflict      = "conflict"
	ReasonGuests        = "guests"
	// ReasonUnconfirmed — спор по категории снят, утверждение отеля расчётом не подтверждено.
	ReasonUnconfirmed = "unconfirmed"
)

// Inbox — дайджест отельера: вопросы, ждущие ответа, по убыванию ценности, и сводка по остальным.
type Inbox struct {
	Hotel Hotel
	// Items — отправленные вопросы, ждущие ответа (open) или уточнения (needs_clarification).
	Items []InboxItem
	// Waiting и LowValue — копятся и в дайджест не попадают: ждут бюджета и ниже порога ценности.
	Waiting, LowValue int
	// SentRecent — отправлено за окно бюджета (без подтверждений пожеланий из броней).
	SentRecent int
	// WishRequests — брони с пожеланием, ждущие подтверждения: вне бюджета и вне Items.
	WishRequests []WishRequest
}

// reasonKind определяет причину вопроса по текущим фактам отеля и спорным категориям.
func reasonKind(q Question, facts []domain.Fact, issues []CategoryIssue, hotelID int64) string {
	kind, _ := reasonNow(q, facts, issues, nil, hotelID)
	return kind
}

// reasonNow — причина вопроса по текущему состоянию. Для категории без открытого конфликта, у которой есть расчёт,
// возвращает ReasonUnconfirmed и текст: сохранённый текст вопроса, написанный при создании, не меняется.
func reasonNow(q Question, facts []domain.Fact, issues, calcs []CategoryIssue, hotelID int64) (string, string) {
	if q.EntityType == domain.EntityRoomCategory {
		for _, is := range issues {
			if strconv.FormatInt(is.CategoryID, 10) == q.EntityID && is.Key == q.Key && is.GuestAgainst > 0 {
				return ReasonGuests, ""
			}
		}
		for _, is := range issues {
			if strconv.FormatInt(is.CategoryID, 10) == q.EntityID && is.Key == q.Key {
				return ReasonConflict, ""
			}
		}
		for _, cs := range calcs {
			if strconv.FormatInt(cs.CategoryID, 10) == q.EntityID && cs.Key == q.Key {
				return ReasonUnconfirmed, fmt.Sprintf("утверждение отеля не подтверждено расчётом (уверенность %s)", strings.Replace(strconv.FormatFloat(cs.Confidence, 'f', 2, 64), ".", ",", 1))
			}
		}
		return ReasonConflict, ""
	}
	id := strconv.FormatInt(hotelID, 10)
	for _, f := range facts {
		if f.Key != q.Key || f.EntityType != domain.EntityHotel || f.EntityID != id {
			continue
		}
		if f.Conflict != nil || f.WeakDisagreement {
			return ReasonConflict, ""
		}
		return ReasonLowConfidence, ""
	}
	return ReasonNoData, ""
}

// MissedDemand — характеристика, которую искали гости, а у отеля её нет или уверенность низкая.
type MissedDemand struct {
	Key, Title string
	Demand     int
	Text       string
	// QuestionID — открытый вопрос по этой характеристике в дайджесте; 0, если вопроса нет.
	QuestionID int64
}

// MissedDemand — упущенный спрос отеля по характеристикам с вопросом отельеру, по убыванию спроса.
func (i *Interviewer) MissedDemand(ctx context.Context, hotelID int64) ([]MissedDemand, error) {
	h, err := i.Store.Hotel(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	demand, err := i.Store.Demand(ctx, h.RegionID)
	if err != nil {
		return nil, err
	}
	facts, err := i.Store.HotelFacts(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	qs, err := i.Store.Questions(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	id := strconv.FormatInt(hotelID, 10)
	var out []MissedDemand
	for _, c := range i.Registry.All() {
		n := demand[c.Key]
		if n == 0 || c.AskHotelier == "" || c.Key == domain.WishGuaranteeKey || c.Level == domain.LevelDistrict {
			continue
		}
		known := false
		for _, f := range facts {
			if f.Key == c.Key && f.EntityType == domain.EntityHotel && f.EntityID == id && f.Confidence >= domain.AskConfidenceMark {
				known = true
			}
		}
		if known {
			continue
		}
		m := MissedDemand{Key: c.Key, Title: c.Title, Demand: n}
		m.Text = guestsSearched(n, c.Title) + "; у вас не указано. " + c.AskHotelier
		for _, q := range qs {
			if q.Key == c.Key && (q.Status == StatusOpen || q.Status == StatusNeedsClarification) {
				m.QuestionID = q.ID
			}
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Demand > out[b].Demand })
	return out, nil
}

// Hotels — демо-отели для переключателя роли.
func (i *Interviewer) Hotels(ctx context.Context) ([]HotelSummary, error) {
	return i.Store.HotelList(ctx)
}

// Inbox собирает дайджест отельера по отелю hotelID.
func (i *Interviewer) Inbox(ctx context.Context, hotelID int64) (Inbox, error) {
	var in Inbox
	h, err := i.Store.Hotel(ctx, hotelID)
	if err != nil {
		return in, err
	}
	in.Hotel = h
	qs, err := i.Store.Questions(ctx, hotelID)
	if err != nil {
		return in, err
	}
	facts, err := i.Store.HotelFacts(ctx, hotelID)
	if err != nil {
		return in, err
	}
	issues, err := i.Store.CategoryIssues(ctx, hotelID)
	if err != nil {
		return in, err
	}
	calcs, err := i.Store.CategoryCalcs(ctx, hotelID)
	if err != nil {
		return in, err
	}
	for _, q := range qs {
		switch q.Status {
		case StatusOpen, StatusNeedsClarification:
			if q.Key == domain.WishGuaranteeKey {
				continue // подтверждения пожеланий идут отдельным блоком WishRequests
			}
			title := q.Key
			if c, ok := i.Registry.Get(q.Key); ok {
				title = c.Title
			}
			kind, reason := reasonNow(q, facts, issues, calcs, hotelID)
			in.Items = append(in.Items, InboxItem{Question: q, Title: title, ReasonKind: kind, Reason: reason})
		case StatusPending:
			in.Waiting++
		case StatusLowValue:
			in.LowValue++
		}
	}
	sort.SliceStable(in.Items, func(a, b int) bool { return in.Items[a].Question.Score.Value > in.Items[b].Question.Score.Value })
	if in.SentRecent, err = i.Store.SentSince(ctx, hotelID, i.now().AddDate(0, 0, -domain.QuestionBudgetDays)); err != nil {
		return in, err
	}
	in.WishRequests, err = i.Store.WishRequests(ctx, hotelID)
	return in, err
}

// AnswerQuestion принимает ответ отельера из кабинета: текст или «да» / «нет».
func (i *Interviewer) AnswerQuestion(ctx context.Context, questionID int64, text string, meta ...AnswerMeta) (*Outcome, error) {
	var m AnswerMeta
	if len(meta) > 0 {
		m = meta[0]
	}
	m.Comment = strings.TrimSpace(m.Comment)
	if utf8.RuneCountInString(m.Comment) > MaxCommentRunes {
		return nil, fmt.Errorf("%w: комментарий длиннее %d символов", ErrInvalid, MaxCommentRunes)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("%w: пустой ответ", ErrInvalid)
	}
	q, err := i.Store.Question(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if q.Status != StatusOpen && q.Status != StatusNeedsClarification {
		return nil, fmt.Errorf("%w (статус %s)", ErrState, q.Status)
	}
	return i.Answer(ctx, q, text, m)
}

// ErrNoPersona — у отеля нет имитированной персоны: ответ ждёт живого отельера.
var ErrNoPersona = errors.New("у отеля нет имитированной персоны")

// PreviewQuestion — ответ персоны на вопрос и его разбор без записи в БД. Вопрос читается из хранилища, а разбор идёт
// тем же путём, что при отправке ответа (processAnswer), поэтому запрос к модели и результат совпадают.
func (i *Interviewer) PreviewQuestion(ctx context.Context, questionID int64) (*Preview, error) {
	q, err := i.Store.Question(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if q.Status != StatusOpen && q.Status != StatusNeedsClarification {
		return nil, fmt.Errorf("%w (статус %s)", ErrState, q.Status)
	}
	pv, err := i.Preview(ctx, q)
	if err != nil {
		return nil, err
	}
	if pv == nil {
		return nil, ErrNoPersona
	}
	return pv, nil
}

// FactOutcome — результат подтверждения или исправления факта.
type FactOutcome struct {
	FactID   int64
	Key      string
	Action   string // confirm | correct | recheck
	Claimed  map[string]any
	Trust    *reputation.Update
	Note     string
	Recheck  bool // факт поставлен на пересчёт, свидетельство не записано
	Evidence bool // записано свидетельство отельера
	// Change — факт до и после записи (с пересчётом репутации); nil у recheck.
	Change *Change
}

// closedResolution объясняет, почему закрылся конфликт: берёт причину, сохранённую движком правды (conflicts.resolution).
// Если вес упал из-за репутации отельера, в текст добавляется её изменение «было → стало».
func (i *Interviewer) closedResolution(ctx context.Context, c *Change, trust *reputation.Update) error {
	if c.Before.Conflict == "" || c.After.Conflict != "" {
		return nil
	}
	res, ok, err := i.Store.LastResolution(ctx, c.EntityType, c.EntityID, c.Key)
	if err != nil || !ok {
		return err
	}
	info := &ResolutionInfo{Kind: res.Kind, Text: res.Text}
	if res.Kind == domain.ResolutionWeightDropped && trust != nil && trust.After < trust.Before && res.Author.Type == domain.AuthorHotelier {
		info.Text = fmt.Sprintf("Спор снят в пользу расчёта: доверие к утверждениям отеля снизилось (%s → %s), их вес в пользу отеля теперь ≈ 0",
			ru2(trust.Before), ru2(trust.After))
	} else {
		info.Text = strings.ToUpper(info.Text[:1]) + info.Text[1:]
	}
	c.Resolution = info
	return nil
}

func ru2(x float64) string { return strings.Replace(fmt.Sprintf("%.2f", x), ".", ",", 1) }

// saveWithReputation записывает свидетельство отельера, пересчитывает его репутацию и собирает факт один раз:
// свидетельство пишется без сборки, репутация считается по судье без автора (не зависит от неё самой), затем факты
// собираются с уже новой репутацией — либо все факты автора, если она изменилась, либо только факт этого свидетельства.
// Без Reputation свидетельство пишется со сборкой факта и репутация не считается (nil).
func (i *Interviewer) saveWithReputation(ctx context.Context, ev domain.Evidence) (*reputation.Update, error) {
	if i.Reputation == nil {
		if err := i.Store.SaveAnswerEvidence(ctx, ev); err != nil {
			return nil, fmt.Errorf("свидетельство отельера: %w", err)
		}
		return nil, nil
	}
	if err := i.Store.WriteAnswerEvidence(ctx, ev); err != nil {
		return nil, fmt.Errorf("свидетельство отельера: %w", err)
	}
	author := domain.Author{Type: ev.AuthorType, ID: ev.AuthorID}
	up, err := i.Reputation.Recalculate(ctx, author)
	if err != nil {
		return nil, fmt.Errorf("репутация: %w", err)
	}
	if up.Changed() {
		err = i.Reputation.Refresh(ctx, author)
	} else {
		err = i.Store.RebuildAnswerScope(ctx, ev)
	}
	if err != nil {
		return nil, fmt.Errorf("сборка факта: %w", err)
	}
	return &up, nil
}

// FactAddress — адрес факта, не зависящий от его id: сущность и характеристика.
type FactAddress struct {
	EntityType, EntityID, Key string
}

// factAt находит активный факт по адресу и проверяет, что он принадлежит отелю hotelID.
func (i *Interviewer) factAt(ctx context.Context, hotelID int64, a FactAddress) (int64, error) {
	snap, err := i.Store.FactAt(ctx, a.EntityType, a.EntityID, a.Key)
	if err != nil {
		return 0, err
	}
	if !snap.Found {
		return 0, fmt.Errorf("%w: нет активного факта %s/%s %s", ErrNotFound, a.EntityType, a.EntityID, a.Key)
	}
	ref, err := i.Store.Fact(ctx, snap.FactID)
	if err != nil {
		return 0, err
	}
	if ref.HotelID != hotelID {
		return 0, fmt.Errorf("%w: факт принадлежит другому отелю", ErrNotFound)
	}
	return snap.FactID, nil
}

// ConfirmFactAt подтверждает факт по адресу «сущность + характеристика» в отеле hotelID.
func (i *Interviewer) ConfirmFactAt(ctx context.Context, hotelID int64, a FactAddress) (*FactOutcome, error) {
	id, err := i.factAt(ctx, hotelID, a)
	if err != nil {
		return nil, err
	}
	return i.ConfirmFact(ctx, id)
}

// CorrectFactAt исправляет факт по адресу «сущность + характеристика» в отеле hotelID.
func (i *Interviewer) CorrectFactAt(ctx context.Context, hotelID int64, a FactAddress, value string, details map[string]any) (*FactOutcome, error) {
	id, err := i.factAt(ctx, hotelID, a)
	if err != nil {
		return nil, err
	}
	return i.CorrectFact(ctx, id, value, details)
}

// ConfirmFact: отельер подтверждает значение факта. Пишется свидетельство отельера с тем же значением; прежнее
// его свидетельство по этой сущности и характеристике заменяется.
func (i *Interviewer) ConfirmFact(ctx context.Context, factID int64) (*FactOutcome, error) {
	ref, err := i.Store.Fact(ctx, factID)
	if err != nil {
		return nil, err
	}
	c, ok := i.Registry.Get(ref.Fact.Key)
	if !ok {
		return nil, fmt.Errorf("%w: нет характеристики %q", ErrInvalid, ref.Fact.Key)
	}
	_, field := fieldFor(c, ref.Fact.EntityType)
	claimed := map[string]any{}
	if isVoting(c, ref.Fact.EntityType) {
		claimed[field] = ref.Fact.Value[field]
	} else {
		for k, v := range ref.Fact.Value {
			claimed[k] = v
		}
	}
	return i.writeFactEvidence(ctx, ref, c, "confirm", claimed)
}

// CorrectFact: отельер исправляет значение факта. Для bool и enum пишется свидетельство отельера с новым значением
// (value из допустимых, details — виды, вес, цена без голосования). Числа не голосуют: значение берётся из расчёта,
// факт ставится на пересчёт, а исправление отельера записывается в журнал как проверка.
func (i *Interviewer) CorrectFact(ctx context.Context, factID int64, value string, details map[string]any) (*FactOutcome, error) {
	ref, err := i.Store.Fact(ctx, factID)
	if err != nil {
		return nil, err
	}
	c, ok := i.Registry.Get(ref.Fact.Key)
	if !ok {
		return nil, fmt.Errorf("%w: нет характеристики %q", ErrInvalid, ref.Fact.Key)
	}
	if !isVoting(c, ref.Fact.EntityType) {
		if err := i.Store.MarkStale(ctx, factID); err != nil {
			return nil, err
		}
		h, _ := i.Store.Hotel(ctx, ref.HotelID)
		s := &session{id: fmt.Sprintf("hotelier-%d-%d", ref.HotelID, i.now().UnixNano())}
		i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("отельер оспорил %s: %q; факт поставлен на пересчёт", c.Key, value), "recheck",
			map[string]any{"fact_id": factID, "value": value, "details": details}, map[string]any{"stale": true})
		return &FactOutcome{FactID: factID, Key: c.Key, Action: "recheck", Recheck: true,
			Note: "числа не голосуют: значение остаётся из расчёта, факт поставлен на пересчёт"}, nil
	}
	values, field := fieldFor(c, ref.Fact.EntityType)
	value = strings.ToLower(strings.TrimSpace(value))
	ok = false
	for _, v := range values {
		ok = ok || v == value
	}
	if !ok {
		return nil, fmt.Errorf("%w: %q, допустимы %v", ErrInvalid, value, values)
	}
	claimed := map[string]any{}
	if c.ValueType == domain.ValueBool && ref.Fact.EntityType != domain.EntityRoomCategory {
		claimed[field] = value == "true"
	} else {
		claimed[field] = value
	}
	if len(details) > 0 {
		claimed["details"] = details
	}
	return i.writeFactEvidence(ctx, ref, c, "correct", claimed)
}

// isVoting — значение характеристики на этом уровне голосует (bool / enum); числа и диапазоны берутся из расчёта.
func isVoting(c domain.Characteristic, entityType string) bool {
	values, _ := fieldFor(c, entityType)
	return len(values) > 0
}

// fieldFor — допустимые значения и поле значения для сущности факта: у категории номеров правило category.
func fieldFor(c domain.Characteristic, entityType string) ([]string, string) {
	if entityType == domain.EntityRoomCategory && c.Category != nil {
		return c.Category.Values, c.Category.ValueField
	}
	return allowedValues(c), c.FieldName()
}

func (i *Interviewer) writeFactEvidence(ctx context.Context, ref FactRef, c domain.Characteristic, action string, claimed map[string]any) (*FactOutcome, error) {
	now := i.now()
	until := now.Add(c.TTL())
	ev := domain.Evidence{
		EntityType: ref.Fact.EntityType, EntityID: ref.Fact.EntityID, Key: c.Key, Claimed: claimed,
		Source: domain.SourceHotelier, Method: HotelierMethod, AuthorType: domain.AuthorHotelier, AuthorID: strconv.FormatInt(ref.HotelID, 10),
		Reliability: domain.HotelierBaseReliability, ObservedAt: now, ValidUntil: &until,
		Details: map[string]any{"fact_id": ref.Fact.ID, "action": action, "note": "подтверждение или исправление факта в кабинете"},
	}
	before, err := i.Store.FactAt(ctx, ref.Fact.EntityType, ref.Fact.EntityID, c.Key)
	if err != nil {
		return nil, fmt.Errorf("факт до записи: %w", err)
	}
	up, err := i.saveWithReputation(ctx, ev)
	if err != nil {
		return nil, err
	}
	out := &FactOutcome{FactID: ref.Fact.ID, Key: c.Key, Action: action, Claimed: claimed, Evidence: true}
	h, _ := i.Store.Hotel(ctx, ref.HotelID)
	s := &session{id: fmt.Sprintf("hotelier-%d-%d", ref.HotelID, now.UnixNano())}
	i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("отельер: %s значения %s", action, c.Key), "cabinet",
		map[string]any{"fact_id": ref.Fact.ID, "entity": ref.Fact.EntityType + "/" + ref.Fact.EntityID}, map[string]any{"claimed": claimed})
	if up != nil {
		out.Trust = up
		i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("репутация отельера: %.2f → %.2f (подтверждено %d из %d)", up.Before, up.After, up.Agree, up.Total),
			"reputation", map[string]any{"author": up.Author.ID}, map[string]any{"before": up.Before, "after": up.After, "agree": up.Agree, "total": up.Total})
	}
	after, err := i.Store.FactAt(ctx, ref.Fact.EntityType, ref.Fact.EntityID, c.Key)
	if err != nil {
		return nil, fmt.Errorf("факт после записи: %w", err)
	}
	out.Change = &Change{EntityType: ref.Fact.EntityType, EntityID: ref.Fact.EntityID, Key: c.Key, Before: before, After: after}
	if err := i.closedResolution(ctx, out.Change, out.Trust); err != nil {
		return nil, fmt.Errorf("причина закрытия конфликта: %w", err)
	}
	return out, nil
}

// Бюджет вопросов для кабинета (константы domain.QuestionBudget и QuestionBudgetDays).
const (
	Budget     = domain.QuestionBudget
	BudgetDays = domain.QuestionBudgetDays
)

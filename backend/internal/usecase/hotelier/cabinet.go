package hotelier

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
}

// Inbox — дайджест отельера: вопросы, ждущие ответа, по убыванию ценности, и сводка по остальным.
type Inbox struct {
	Hotel Hotel
	// Items — отправленные вопросы, ждущие ответа (open) или уточнения (needs_clarification).
	Items []InboxItem
	// Waiting и LowValue — копятся и в дайджест не попадают: ждут бюджета и ниже порога ценности.
	Waiting, LowValue int
	// SentRecent — отправлено за окно бюджета.
	SentRecent int
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
	for _, q := range qs {
		switch q.Status {
		case StatusOpen, StatusNeedsClarification:
			title := q.Key
			if c, ok := i.Registry.Get(q.Key); ok {
				title = c.Title
			}
			in.Items = append(in.Items, InboxItem{Question: q, Title: title})
		case StatusPending:
			in.Waiting++
		case StatusLowValue:
			in.LowValue++
		}
	}
	sort.SliceStable(in.Items, func(a, b int) bool { return in.Items[a].Question.Score.Value > in.Items[b].Question.Score.Value })
	in.SentRecent, err = i.Store.SentSince(ctx, hotelID, i.now().AddDate(0, 0, -domain.QuestionBudgetDays))
	return in, err
}

// AnswerQuestion принимает ответ отельера из кабинета: текст или «да» / «нет».
func (i *Interviewer) AnswerQuestion(ctx context.Context, questionID int64, text string) (*Outcome, error) {
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
	return i.Answer(ctx, q, text)
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
	if err := i.Store.SaveAnswerEvidence(ctx, ev); err != nil {
		return nil, fmt.Errorf("свидетельство отельера: %w", err)
	}
	out := &FactOutcome{FactID: ref.Fact.ID, Key: c.Key, Action: action, Claimed: claimed, Evidence: true}
	h, _ := i.Store.Hotel(ctx, ref.HotelID)
	s := &session{id: fmt.Sprintf("hotelier-%d-%d", ref.HotelID, now.UnixNano())}
	i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("отельер: %s значения %s", action, c.Key), "cabinet",
		map[string]any{"fact_id": ref.Fact.ID, "entity": ref.Fact.EntityType + "/" + ref.Fact.EntityID}, map[string]any{"claimed": claimed})
	if i.Reputation != nil {
		up, err := i.Reputation.Update(ctx, domain.HotelierAuthor(ref.HotelID))
		if err != nil {
			return nil, fmt.Errorf("репутация: %w", err)
		}
		out.Trust = &up
		i.trace(ctx, s, h, agent.ModePlanner, fmt.Sprintf("репутация отельера: %.2f → %.2f (подтверждено %d из %d)", up.Before, up.After, up.Agree, up.Total),
			"reputation", map[string]any{"author": up.Author.ID}, map[string]any{"before": up.Before, "after": up.After, "agree": up.Agree, "total": up.Total})
	}
	return out, nil
}

// Бюджет вопросов для кабинета (константы domain.QuestionBudget и QuestionBudgetDays).
const (
	Budget     = domain.QuestionBudget
	BudgetDays = domain.QuestionBudgetDays
)

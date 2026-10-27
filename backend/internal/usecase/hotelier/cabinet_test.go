package hotelier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/reputation"
)

func TestInboxSortedByValueWithCounters(t *testing.T) {
	st := baseStore()
	iv := newInterviewer(t, st, nil)
	// Бюджет в 2 вопроса: третий (аптека, ниже порога) копится, четвёртого нет.
	st.sentTimes = []time.Time{testNow.AddDate(0, 0, -1)}
	if _, err := iv.Round(context.Background(), 5, Options{}); err != nil {
		t.Fatal(err)
	}
	in, err := iv.Inbox(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Items) != 2 || in.LowValue != 1 || in.SentRecent != 3 {
		t.Fatalf("дайджест: %d вопросов, ниже порога %d, отправлено за окно %d", len(in.Items), in.LowValue, in.SentRecent)
	}
	if in.Items[0].Question.Score.Value < in.Items[1].Question.Score.Value || in.Items[0].Title == "" {
		t.Errorf("порядок по ценности: %+v", in.Items)
	}
	if in.Items[0].Question.Reason == "" {
		t.Error("у вопроса есть причина")
	}
}

func TestAnswerQuestionStates(t *testing.T) {
	st := baseStore()
	iv := newInterviewer(t, st, nil)
	if _, err := iv.Round(context.Background(), 5, Options{}); err != nil {
		t.Fatal(err)
	}
	var open Question
	for _, q := range st.qs {
		if q.Status == StatusOpen && q.Key == "pets_allowed" {
			open = q
		}
	}
	if _, err := iv.AnswerQuestion(context.Background(), open.ID, "  "); !errors.Is(err, ErrInvalid) {
		t.Errorf("пустой ответ: %v", err)
	}
	out, err := iv.AnswerQuestion(context.Background(), open.ID, "Да, с собаками до 8 кг, 20 € за ночь")
	if err != nil || out.Status != StatusAnswered || len(st.evidence) != 1 {
		t.Fatalf("ответ: %+v, %v", out, err)
	}
	// Повторно на тот же вопрос ответить нельзя.
	if _, err := iv.AnswerQuestion(context.Background(), open.ID, "Да"); !errors.Is(err, ErrState) {
		t.Errorf("повторный ответ: %v", err)
	}
	if _, err := iv.AnswerQuestion(context.Background(), 999, "Да"); !errors.Is(err, ErrNotFound) {
		t.Errorf("нет вопроса: %v", err)
	}
}

func TestConfirmAndCorrectFact(t *testing.T) {
	st := baseStore()
	st.refs = map[int64]FactRef{
		1: {HotelID: 5, Fact: domain.Fact{ID: 1, EntityType: domain.EntityHotel, EntityID: "5", Key: "pets_allowed", Value: map[string]any{"available": true}}},
		2: {HotelID: 5, Fact: domain.Fact{ID: 2, EntityType: domain.EntityHotel, EntityID: "5", Key: "walk_to_beach", Value: map[string]any{"minutes": 12.0}}},
	}
	reg, err := domain.NewRegistry([]domain.Characteristic{
		testRegistryChar(t, "pets_allowed"),
		{Key: "walk_to_beach", Title: "Пляж пешком", Level: domain.LevelHotel, ValueType: domain.ValueNumber, ValueField: "minutes", TTLDays: 365},
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := &Interviewer{Registry: reg, Store: st, Now: func() time.Time { return testNow }}

	out, err := iv.ConfirmFact(context.Background(), 1)
	if err != nil || !out.Evidence || st.evidence[0].Claimed["available"] != true {
		t.Fatalf("подтверждение: %+v %v", out, err)
	}
	out, err = iv.CorrectFact(context.Background(), 1, "false", map[string]any{"note": "закрыли"})
	if err != nil || st.evidence[1].Claimed["available"] != false {
		t.Fatalf("исправление: %+v %v", out, err)
	}
	if _, err := iv.CorrectFact(context.Background(), 1, "возможно", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("недопустимое значение: %v", err)
	}
	// Числа не голосуют: свидетельство не пишется, факт ставится на пересчёт.
	before := len(st.evidence)
	out, err = iv.CorrectFact(context.Background(), 2, "5", nil)
	if err != nil || !out.Recheck || out.Evidence || len(st.evidence) != before || len(st.stale) != 1 || st.stale[0] != 2 {
		t.Errorf("число: %+v %v, пересчёт %v", out, err, st.stale)
	}
}

func TestAnswerReportsFactBeforeAndAfter(t *testing.T) {
	st := baseStore()
	st.refs = map[int64]FactRef{1: {HotelID: 5, Fact: domain.Fact{ID: 1, EntityType: domain.EntityHotel, EntityID: "5", Key: "pets_allowed",
		Value: map[string]any{"available": false}, Confidence: 0.5}}}
	st.afterSnap = &FactSnap{Found: true, FactID: 9, Value: map[string]any{"available": true}, Confidence: 0.6}
	iv := newInterviewer(t, st, nil)
	if _, err := iv.Round(context.Background(), 5, Options{}); err != nil {
		t.Fatal(err)
	}
	var open Question
	for _, q := range st.qs {
		if q.Status == StatusOpen && q.Key == "pets_allowed" {
			open = q
		}
	}
	out, err := iv.AnswerQuestion(context.Background(), open.ID, "Да, с собаками")
	if err != nil || out.Change == nil {
		t.Fatalf("ответ: %+v %v", out, err)
	}
	if !out.Change.Before.Found || out.Change.Before.Value["available"] != false || out.Change.After.Value["available"] != true || out.Change.After.Confidence != 0.6 {
		t.Errorf("до и после: %+v", out.Change)
	}
}

func TestFactAtChecksHotelOwnership(t *testing.T) {
	st := baseStore()
	st.refs = map[int64]FactRef{1: {HotelID: 5, Fact: domain.Fact{ID: 1, EntityType: domain.EntityHotel, EntityID: "5", Key: "pets_allowed",
		Value: map[string]any{"available": true}}}}
	reg, err := domain.NewRegistry([]domain.Characteristic{testRegistryChar(t, "pets_allowed")})
	if err != nil {
		t.Fatal(err)
	}
	iv := &Interviewer{Registry: reg, Store: st, Now: func() time.Time { return testNow }}
	addr := FactAddress{EntityType: domain.EntityHotel, EntityID: "5", Key: "pets_allowed"}
	if _, err := iv.ConfirmFactAt(context.Background(), 5, addr); err != nil || len(st.evidence) != 1 {
		t.Fatalf("свой факт: %v", err)
	}
	if _, err := iv.ConfirmFactAt(context.Background(), 6, addr); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой отель: %v", err)
	}
	if _, err := iv.ConfirmFactAt(context.Background(), 5, FactAddress{EntityType: domain.EntityHotel, EntityID: "5", Key: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("нет факта: %v", err)
	}
}

func TestReasonKind(t *testing.T) {
	facts := []domain.Fact{
		{Key: "a", EntityType: domain.EntityHotel, EntityID: "5", Confidence: 0.4},
		{Key: "b", EntityType: domain.EntityHotel, EntityID: "5", Confidence: 0.7, Conflict: &domain.Conflict{Description: "спор"}},
	}
	issues := []CategoryIssue{{CategoryID: 3, Key: "sea_view", GuestAgainst: 2}, {CategoryID: 4, Key: "sea_view"}}
	cat := func(id string) Question {
		return Question{Key: "sea_view", EntityType: domain.EntityRoomCategory, EntityID: id}
	}
	for _, c := range []struct {
		q    Question
		want string
	}{
		{Question{Key: "a"}, ReasonLowConfidence},
		{Question{Key: "b"}, ReasonConflict},
		{Question{Key: "c"}, ReasonNoData},
		{cat("3"), ReasonGuests},
		{cat("4"), ReasonConflict},
	} {
		if got := reasonKind(c.q, facts, issues, 5); got != c.want {
			t.Errorf("%s/%s: %s, ожидалось %s", c.q.Key, c.q.EntityID, got, c.want)
		}
	}
}

func TestAnswerExplainsClosedConflictByLostWeight(t *testing.T) {
	st := baseStore()
	st.refs = map[int64]FactRef{1: {HotelID: 5, Fact: domain.Fact{ID: 1, EntityType: domain.EntityHotel, EntityID: "5", Key: "pets_allowed",
		Value: map[string]any{"available": false}, Confidence: 0.5}}}
	st.afterSnap = &FactSnap{Found: true, FactID: 9, Value: map[string]any{"available": false}, Confidence: 0.85}
	st.resolution = domain.ConflictResolution{Kind: domain.ResolutionWeightDropped, Text: "спор снят в пользу расчёта", Author: domain.Author{Type: domain.AuthorHotelier}}
	iv := newInterviewer(t, st, nil)
	c := &Change{EntityType: domain.EntityHotel, EntityID: "5", Key: "pets_allowed",
		Before: FactSnap{Found: true, Conflict: "спор"}, After: FactSnap{Found: true}}
	trust := &reputation.Update{Before: 0.5, After: 1.0 / 3}
	if err := iv.closedResolution(context.Background(), c, trust); err != nil {
		t.Fatal(err)
	}
	want := "Спор снят в пользу расчёта: доверие к утверждениям отеля снизилось (0,50 → 0,33), их вес в пользу отеля теперь ≈ 0"
	if c.Resolution == nil || c.Resolution.Kind != domain.ResolutionWeightDropped || c.Resolution.Text != want {
		t.Errorf("причина: %+v", c.Resolution)
	}
	// Конфликт не закрывался — причины нет.
	open := &Change{Before: FactSnap{Conflict: "спор"}, After: FactSnap{Conflict: "спор"}}
	if err := iv.closedResolution(context.Background(), open, trust); err != nil || open.Resolution != nil {
		t.Errorf("открытый конфликт: %+v %v", open.Resolution, err)
	}
}

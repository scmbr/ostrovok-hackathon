package hotelier

import (
	"context"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func wishInterviewer(t *testing.T, st *fakeStore) *Interviewer {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: domain.WishGuaranteeKey, Title: "Гарантирует пожелание", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available",
			TTLDays: 180, FavorableValues: []string{"true"}, AskHotelier: "Можете гарантировать пожелание?"},
		{Key: "pets_allowed", Title: "Можно с питомцем", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 180,
			FavorableValues: []string{"true"}, AskHotelier: "Принимаете животных?"},
		{Key: "no_question", Title: "Без вопроса", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 365},
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := newInterviewer(t, st, nil)
	iv.Registry = reg
	return iv
}

// Вопрос wish_guarantee один на отель: вторая бронь с пожеланием переиспользует открытый вопрос.
func TestConfirmWishReusesQuestion(t *testing.T) {
	st := baseStore()
	iv := wishInterviewer(t, st)
	ctx := context.Background()
	first, err := iv.ConfirmWish(ctx, 5, "Standard", "восточная сторона, этаж 4+")
	if err != nil {
		t.Fatal(err)
	}
	if first.Question == nil || !first.Created || !first.Sent || first.Question.Format != domain.FormatConfirm {
		t.Fatalf("первая бронь: %+v", first)
	}
	second, err := iv.ConfirmWish(ctx, 5, "Deluxe", "южная сторона")
	if err != nil {
		t.Fatal(err)
	}
	if second.Question == nil || second.Created || !second.Reused || second.Question.ID != first.Question.ID || len(st.qs) != 1 {
		t.Fatalf("вторая бронь: %+v, вопросов %d", second, len(st.qs))
	}
}

// Копящийся вопрос wish_guarantee отправляется сразу, в обход недельного бюджета.
func TestConfirmWishPromotesWaitingQuestion(t *testing.T) {
	st := baseStore()
	st.qs = []Question{{ID: 1, HotelID: 5, Key: domain.WishGuaranteeKey, Status: StatusLowValue}}
	// бюджет исчерпан: три вопроса отправлены на этой неделе
	for k := 0; k < 3; k++ {
		st.sentTimes = append(st.sentTimes, testNow)
	}
	out, err := wishInterviewer(t, st).ConfirmWish(context.Background(), 5, "Standard", "этаж 4+")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Sent || out.Question.ID != 1 || st.qs[0].Status != StatusOpen || len(st.qs) != 1 {
		t.Fatalf("%+v, вопросы %+v", out, st.qs)
	}
}

// Ответ отельера: «да» → available = true, «нет» → available = false; значение берётся из ответа, а не из расчёта.
func TestWishGuaranteeAnswers(t *testing.T) {
	for text, want := range map[string]bool{"Да, гарантируем": true, "Нет": false} {
		st := baseStore()
		iv := wishInterviewer(t, st)
		ctx := context.Background()
		ask, err := iv.ConfirmWish(ctx, 5, "Standard", "этаж 4+")
		if err != nil {
			t.Fatal(err)
		}
		out, err := iv.AnswerQuestion(ctx, ask.Question.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != StatusAnswered || len(st.evidence) != 1 || st.evidence[0].Claimed["available"] != want || st.evidence[0].Key != domain.WishGuaranteeKey {
			t.Errorf("%q: статус %s, свидетельства %+v", text, out.Status, st.evidence)
		}
	}
}

// Отельер уже отвечал: вопрос не создаётся, ответ возвращается.
func TestConfirmWishAlreadyAnswered(t *testing.T) {
	st := baseStore()
	st.facts = []domain.Fact{{EntityType: domain.EntityHotel, EntityID: "5", Key: domain.WishGuaranteeKey, Value: map[string]any{"available": false}, Confidence: 0.9}}
	out, err := wishInterviewer(t, st).ConfirmWish(context.Background(), 5, "Standard", "этаж 4+")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Answered || out.Guarantees || out.Question != nil || len(st.qs) != 0 {
		t.Fatalf("%+v", out)
	}
}

// «Спросить отель»: живой вопрос переиспользуется (в том числе вопрос о категории), дубль не создаётся.
func TestAskAboutNoDuplicate(t *testing.T) {
	st := baseStore()
	st.qs = []Question{{ID: 7, HotelID: 5, Key: "pets_allowed", Status: StatusOpen, EntityType: domain.EntityRoomCategory, EntityID: "9"}}
	iv := wishInterviewer(t, st)
	out, err := iv.AskAbout(context.Background(), 5, "pets_allowed")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Reused || out.Created || !out.Sent || out.Question.ID != 7 || len(st.qs) != 1 {
		t.Fatalf("%+v, вопросы %d", out, len(st.qs))
	}
	if no, err := iv.AskAbout(context.Background(), 5, "no_question"); err != nil || no.Question != nil || no.Note == "" {
		t.Errorf("без вопроса отельеру: %+v, %v", no, err)
	}
}

// Новый вопрос по запросу гостя идёт через обычный раунд: при данных, которых нет, вопрос создаётся.
func TestAskAboutCreatesThroughRound(t *testing.T) {
	st := baseStore()
	out, err := wishInterviewer(t, st).AskAbout(context.Background(), 5, "pets_allowed")
	if err != nil {
		t.Fatal(err)
	}
	if out.Question == nil || !out.Created || out.Question.Key != "pets_allowed" {
		t.Fatalf("%+v", out)
	}
}

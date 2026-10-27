package hotelier

import (
	"context"
	"errors"
	"fmt"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Вопросы отельеру по инициативе гостя: «спросить отель» в контракте ожиданий и подтверждение пожелания к брони.
// Вопрос по-прежнему про характеристику отеля, а не от имени гостя: один ответ для всех будущих гостей.

// Ask — результат запроса вопроса отельеру.
type Ask struct {
	// Question — найденный или созданный вопрос; nil — вопрос не нужен.
	Question *Question
	// Created — вопрос создан этим запросом; Reused — уже был (открытый или копящийся).
	Created, Reused bool
	// Sent — вопрос открыт у отельера (в дайджесте); false — копится и ждёт бюджета или порога ценности.
	Sent bool
	// Answered и Guarantees — для wish_guarantee: отельер уже отвечал (факт есть), и каков ответ.
	Answered, Guarantees bool
	Note                 string
}

func live(status string) bool {
	switch status {
	case StatusPending, StatusLowValue, StatusOpen, StatusNeedsClarification:
		return true
	}
	return false
}

// findLive — живой вопрос по характеристике (любая сущность); открытый важнее копящегося.
func findLive(qs []Question, key string) *Question {
	var found *Question
	for k := range qs {
		q := &qs[k]
		if q.Key != key || !live(q.Status) {
			continue
		}
		if found == nil || (q.Status == StatusOpen || q.Status == StatusNeedsClarification) && !(found.Status == StatusOpen || found.Status == StatusNeedsClarification) {
			found = q
		}
	}
	return found
}

func sentToHotelier(q *Question) bool {
	return q != nil && (q.Status == StatusOpen || q.Status == StatusNeedsClarification)
}

// AskAbout создаёт или находит вопрос отельеру по характеристике («спросить отель» в контракте). Дубль живого вопроса
// не создаётся. Нового вопроса нет в двух случаях: у характеристики нет вопроса отельеру или данных достаточно
// (тогда решает обычный раунд интервьюера: ценность, порог и недельный бюджет).
func (i *Interviewer) AskAbout(ctx context.Context, hotelID int64, key string) (Ask, error) {
	c, ok := i.Registry.Get(key)
	if !ok {
		return Ask{}, fmt.Errorf("нет характеристики %q", key)
	}
	if c.AskHotelier == "" {
		return Ask{Note: "по этой характеристике отельера не спрашиваем"}, nil
	}
	qs, err := i.Store.Questions(ctx, hotelID)
	if err != nil {
		return Ask{}, err
	}
	if q := findLive(qs, key); q != nil {
		return Ask{Question: q, Reused: true, Sent: sentToHotelier(q), Note: waitNote(q)}, nil
	}
	if _, err := i.Round(ctx, hotelID, Options{}); err != nil {
		return Ask{}, fmt.Errorf("раунд вопросов: %w", err)
	}
	if qs, err = i.Store.Questions(ctx, hotelID); err != nil {
		return Ask{}, err
	}
	q := findLive(qs, key)
	if q == nil {
		return Ask{Note: "вопрос пока не нужен: данных достаточно по правилам системы"}, nil
	}
	return Ask{Question: q, Created: true, Sent: sentToHotelier(q), Note: waitNote(q)}, nil
}

func waitNote(q *Question) string {
	if sentToHotelier(q) {
		return ""
	}
	return "вопрос копится: отель получит его в дайджесте, когда позволит недельный бюджет"
}

// ConfirmWish подготавливает вопрос формата confirm «гость бронирует <категория> с пожеланием <…>: можете гарантировать?»
// по характеристике wish_guarantee. Вопрос один на отель: открытый или копящийся переиспользуется, копящийся
// сразу отправляется. Бюджет недельных вопросов на такие подтверждения не распространяется: это естественный момент
// общения с отельером (бронь с пожеланием). Если отельер уже отвечал, вопрос не создаётся — ответ возвращается.
func (i *Interviewer) ConfirmWish(ctx context.Context, hotelID int64, category, wish string) (Ask, error) {
	c, ok := i.Registry.Get(domain.WishGuaranteeKey)
	if !ok {
		return Ask{}, errors.New("в реестре нет wish_guarantee")
	}
	h, err := i.Store.Hotel(ctx, hotelID)
	if err != nil {
		return Ask{}, err
	}
	answered, guarantees, err := i.WishAnswer(ctx, hotelID)
	if err != nil {
		return Ask{}, err
	}
	if answered {
		return Ask{Answered: true, Guarantees: guarantees}, nil
	}
	qs, err := i.Store.Questions(ctx, hotelID)
	if err != nil {
		return Ask{}, err
	}
	q := findLive(qs, c.Key)
	if sentToHotelier(q) {
		return Ask{Question: q, Reused: true, Sent: true}, nil
	}
	text := fmt.Sprintf("Гость бронирует «%s» с пожеланием «%s»: можете гарантировать?", category, wish)
	cand := Question{HotelID: hotelID, Key: c.Key, Text: text, Format: domain.FormatConfirm, Status: StatusPending,
		Reason: "бронь с пожеланием: гость просит закрепить сторону или этаж",
		Score:  Score{Value: 1, Uncertainty: 1, Impact: 1, DemandNorm: 1}}
	reused := q != nil
	if reused {
		cand.ID = q.ID
	}
	saved, err := i.Store.UpsertCandidate(ctx, cand)
	if err != nil {
		return Ask{}, err
	}
	at := i.now()
	if err := i.Store.Send(ctx, saved.ID, text, domain.FormatConfirm, h.Persona, at); err != nil {
		return Ask{}, fmt.Errorf("отправка вопроса: %w", err)
	}
	saved.Text, saved.Status, saved.Format, saved.Persona, saved.SentAt = text, StatusOpen, domain.FormatConfirm, h.Persona, &at
	return Ask{Question: &saved, Created: !reused, Reused: reused, Sent: true}, nil
}

// WishAnswer сообщает, отвечал ли отельер на вопрос о пожеланиях (факт wish_guarantee) и каков ответ.
func (i *Interviewer) WishAnswer(ctx context.Context, hotelID int64) (answered, guarantees bool, err error) {
	facts, err := i.Store.HotelFacts(ctx, hotelID)
	if err != nil {
		return false, false, err
	}
	c, ok := i.Registry.Get(domain.WishGuaranteeKey)
	if !ok {
		return false, false, nil
	}
	if f := hotelFact(facts, c.Key, hotelID); f != nil && !f.Stale {
		if v, ok := f.Value[c.FieldName()].(bool); ok {
			return true, v, nil
		}
	}
	return false, false, nil
}

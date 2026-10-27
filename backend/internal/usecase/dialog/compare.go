package dialog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// CompareEvent — результат команды «сравни …».
type CompareEvent struct {
	Compared booking.Compared
}

// turnCompare обрабатывает команду «сравни первый и третий / A и B / шортлист» без модели. handled == false —
// это не команда, ход идёт обычным путём (и кеш диалога с моделью не затрагивается).
func (e *Engine) turnCompare(ctx context.Context, req Request, emit func(Event)) (handled bool, err error) {
	if e.Compare == nil || req.Message == "" || req.Choice != "" {
		return false, nil
	}
	cmd, ok := domain.ParseCompareCommand(req.Message)
	if !ok {
		return false, nil
	}
	r, err := e.newRun(ctx, req, ModeRule, emit)
	if err != nil {
		return true, err
	}
	finish := func(text string) (bool, error) {
		emit(Event{EventMessage, MessageEvent{Text: text, Source: "template"}})
		if err := e.save(ctx, req.SessionID, r.st); err != nil {
			return true, err
		}
		emit(Event{EventDone, DoneEvent{SessionID: req.SessionID, Mode: ModeRule, Asked: r.st.Asked}})
		return true, r.tr.err
	}
	ids, note, msg, err := e.compareTargets(ctx, cmd, r.st)
	if err != nil {
		return true, err
	}
	if msg != "" {
		r.tr.step("команда сравнения не выполнена: "+msg, "compare", map[string]any{"text": req.Message}, nil)
		return finish(msg)
	}
	c, err := e.Compare.Compare(ctx, booking.CompareRequest{SessionID: req.SessionID, HotelIDs: ids})
	var ve *search.ValidationError
	switch {
	case errors.Is(err, booking.ErrInvalid), errors.As(err, &ve), errors.Is(err, booking.ErrNotFound):
		m := "Не получилось сравнить: " + strings.TrimPrefix(err.Error(), booking.ErrInvalid.Error()+": ")
		r.tr.step("команда сравнения отклонена: "+err.Error(), "compare", map[string]any{"hotel_ids": ids}, nil)
		return finish(m)
	case err != nil:
		return true, err
	}
	r.tr.step(fmt.Sprintf("команда «сравни»: %d отеля, вывод: %s", len(ids), c.Comparison.Verdict), "compare",
		map[string]any{"text": req.Message, "hotel_ids": ids}, map[string]any{"winner": c.Comparison.Winner})
	emit(Event{EventCompare, CompareEvent{Compared: c}})
	text := c.Comparison.Verdict
	if c.Comparison.Tradeoff != "" {
		text += " " + c.Comparison.Tradeoff
	}
	if note != "" {
		text = note + " " + text
	}
	return finish(text)
}

// compareTargets переводит команду в id отелей. msg != "" — гостю нужно ответить, а не сравнивать.
func (e *Engine) compareTargets(ctx context.Context, cmd domain.CompareCommand, st State) (ids []int64, note, msg string, err error) {
	if cmd.Shortlist {
		ids = st.Shortlist
		if len(ids) < 2 {
			return nil, "", "В шортлисте меньше двух отелей: добавьте ещё отели, чтобы сравнить.", nil
		}
		if len(ids) > domain.MaxCompareSides {
			ids = ids[:domain.MaxCompareSides]
			note = fmt.Sprintf("В шортлисте больше %d отелей, сравниваю первые %d.", domain.MaxCompareSides, domain.MaxCompareSides)
		}
		return ids, note, "", nil
	}
	if len(cmd.Refs) < 2 {
		return nil, "", "Назовите два или три отеля для сравнения: по номеру в выдаче («сравни первый и третий») или по названию.", nil
	}
	if len(cmd.Refs) > domain.MaxCompareSides {
		return nil, "", fmt.Sprintf("Сравнить можно от двух до %d отелей за раз.", domain.MaxCompareSides), nil
	}
	cands, err := e.Search.Hotels(ctx)
	if err != nil {
		return nil, "", "", err
	}
	for _, ref := range cmd.Refs {
		s, ok := resolveSimilar(ref, st, cands)
		if !ok {
			what := ref.Name
			if ref.Ordinal > 0 {
				what = fmt.Sprintf("номер %d в выдаче", ref.Ordinal)
			}
			return nil, "", fmt.Sprintf("Не поняла, какой отель имеется в виду: «%s». Назовите отель из выдачи по номеру или названию.", what), nil
		}
		ids = append(ids, s.ID)
	}
	return ids, "", "", nil
}

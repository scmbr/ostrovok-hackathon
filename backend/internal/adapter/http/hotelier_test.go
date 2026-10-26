package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
)

type fakeCabinet struct {
	answered string
	err      error
}

func (f *fakeCabinet) Inbox(_ context.Context, id int64) (hotelier.Inbox, error) {
	if f.err != nil {
		return hotelier.Inbox{}, f.err
	}
	return hotelier.Inbox{
		Hotel: hotelier.Hotel{ID: id, Name: "Hotel Test"}, Waiting: 1, LowValue: 2, SentRecent: 2,
		Items: []hotelier.InboxItem{{Title: "Тишина ночью", Question: hotelier.Question{
			ID: 7, Key: "noise_night", Format: domain.FormatConfirm, Text: "Верно?", Reason: "36 гостей искали «Тишина ночью»; уверенность 0,79",
			Status: hotelier.StatusOpen, Score: hotelier.Score{Value: 0.4, Demand: 36},
		}}},
	}, nil
}

func (f *fakeCabinet) AnswerQuestion(_ context.Context, id int64, text string) (*hotelier.Outcome, error) {
	f.answered = text
	if f.err != nil {
		return nil, f.err
	}
	return &hotelier.Outcome{Question: hotelier.Question{ID: id}, Status: hotelier.StatusAnswered, Parsed: hotelier.Parsed{Found: true, Mode: "planner"}}, nil
}

func (f *fakeCabinet) ConfirmFact(_ context.Context, id int64) (*hotelier.FactOutcome, error) {
	return &hotelier.FactOutcome{FactID: id, Key: "pets_allowed", Action: "confirm", Evidence: true}, f.err
}

func (f *fakeCabinet) CorrectFact(_ context.Context, id int64, value string, _ map[string]any) (*hotelier.FactOutcome, error) {
	return &hotelier.FactOutcome{FactID: id, Key: "pets_allowed", Action: "correct", Claimed: map[string]any{"available": value == "true"}, Evidence: true}, f.err
}

func do(h http.Handler, method, url, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, url, strings.NewReader(body)))
	return rec
}

func TestHotelierInbox(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, &fakeCabinet{})
	rec := do(h, "GET", "/api/hotelier/5/inbox", "")
	if rec.Code != 200 {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body)
	}
	var out inboxDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.HotelID != 5 || out.Budget.Limit != 3 || out.Budget.WindowDays != 7 || out.Budget.Sent != 2 || out.Waiting != 1 || out.LowValue != 2 {
		t.Errorf("сводка: %+v", out)
	}
	if len(out.Items) != 1 || out.Items[0].Reason == "" || out.Items[0].Format != "confirm" || out.Items[0].Value != 0.4 {
		t.Errorf("вопрос в дайджесте: %+v", out.Items)
	}
	if rec := do(h, "GET", "/api/hotelier/abc/inbox", ""); rec.Code != 400 {
		t.Errorf("некорректный id: %d", rec.Code)
	}
}

func TestHotelierAnswer(t *testing.T) {
	svc := &fakeCabinet{}
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, svc)
	for body, want := range map[string]string{`{"answer":"yes"}`: "да", `{"answer":"no"}`: "нет", `{"text":"Да, до 5 кг"}`: "Да, до 5 кг"} {
		if rec := do(h, "POST", "/api/hotelier/questions/7/answer", body); rec.Code != 200 || svc.answered != want {
			t.Errorf("%s: статус %d, текст %q", body, rec.Code, svc.answered)
		}
	}
	if rec := do(h, "POST", "/api/hotelier/questions/7/answer", `{"answer":"maybe"}`); rec.Code != 400 {
		t.Errorf("answer maybe: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/hotelier/questions/7/answer", `не json`); rec.Code != 400 {
		t.Errorf("не JSON: %d", rec.Code)
	}
}

func TestHotelierErrorsMapToStatuses(t *testing.T) {
	cases := map[error]int{
		hotelier.ErrNotFound: 404,
		fmt.Errorf("%w (статус closed)", hotelier.ErrState): 409,
		fmt.Errorf("%w: пусто", hotelier.ErrInvalid):        400,
	}
	for err, want := range cases {
		h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, &fakeCabinet{err: err})
		if rec := do(h, "POST", "/api/hotelier/questions/7/answer", `{"text":"да"}`); rec.Code != want {
			t.Errorf("%v: %d, ожидалось %d", err, rec.Code, want)
		}
	}
}

func TestHotelierFacts(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, &fakeCabinet{})
	if rec := do(h, "POST", "/api/hotelier/facts/3/confirm", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"evidence_written":true`) {
		t.Errorf("confirm: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/hotelier/facts/3/correct", `{"value":"false","details":{"note":"x"}}`); rec.Code != 200 {
		t.Errorf("correct: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/hotelier/facts/3/correct", `{}`); rec.Code != 400 {
		t.Errorf("correct без value: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/hotelier/facts/3/delete", ""); rec.Code != 404 {
		t.Errorf("неизвестное действие: %d", rec.Code)
	}
}

// Без кабинета маршрутов нет: /api/hotelier/* отвечает 404 JSON.
func TestHotelierRoutesAbsentWithoutCabinet(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil)
	if rec := do(h, "GET", "/api/hotelier/5/inbox", ""); rec.Code != 404 {
		t.Errorf("%d", rec.Code)
	}
}

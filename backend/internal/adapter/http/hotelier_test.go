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
	addr     hotelier.FactAddress
	meta     hotelier.AnswerMeta
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

func (f *fakeCabinet) AnswerQuestion(_ context.Context, id int64, text string, meta ...hotelier.AnswerMeta) (*hotelier.Outcome, error) {
	f.answered = text
	if len(meta) > 0 {
		f.meta = meta[0]
	}
	if f.err != nil {
		return nil, f.err
	}
	return &hotelier.Outcome{Question: hotelier.Question{ID: id}, Status: hotelier.StatusAnswered, Parsed: hotelier.Parsed{Found: true, Mode: "planner"}}, nil
}

func (f *fakeCabinet) PreviewQuestion(_ context.Context, id int64) (*hotelier.Preview, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &hotelier.Preview{Question: hotelier.Question{ID: id}, Persona: "embellishing", Answer: "Да, у нас всё тихо",
		Parsed: hotelier.Parsed{Found: true, Mode: "cache", Claimed: map[string]any{"level": "quiet"}}}, nil
}

func (f *fakeCabinet) ConfirmFact(_ context.Context, id int64) (*hotelier.FactOutcome, error) {
	return &hotelier.FactOutcome{FactID: id, Key: "pets_allowed", Action: "confirm", Evidence: true}, f.err
}

func (f *fakeCabinet) CorrectFact(_ context.Context, id int64, value string, _ map[string]any) (*hotelier.FactOutcome, error) {
	return &hotelier.FactOutcome{FactID: id, Key: "pets_allowed", Action: "correct", Claimed: map[string]any{"available": value == "true"}, Evidence: true}, f.err
}

func (f *fakeCabinet) Hotels(context.Context) ([]hotelier.HotelSummary, error) {
	return []hotelier.HotelSummary{{ID: 326, Name: "Hotel 326", Persona: "embellishing"}}, f.err
}

func (f *fakeCabinet) MissedDemand(_ context.Context, id int64) ([]hotelier.MissedDemand, error) {
	return []hotelier.MissedDemand{{Key: "noise_night", Title: "Тишина ночью", Demand: 14, Text: "14 гостей искали", QuestionID: 7}}, f.err
}

var changeFixture = &hotelier.Change{EntityType: "hotel", EntityID: "5", Key: "pets_allowed",
	Before: hotelier.FactSnap{Found: true, Confidence: 0.5, Value: map[string]any{"available": false}},
	After:  hotelier.FactSnap{Found: true, Confidence: 0.6, Value: map[string]any{"available": true}, Conflict: "спор"}}

func (f *fakeCabinet) ConfirmFactAt(_ context.Context, hotelID int64, a hotelier.FactAddress) (*hotelier.FactOutcome, error) {
	f.addr = a
	return &hotelier.FactOutcome{FactID: 1, Key: a.Key, Action: "confirm", Evidence: true, Change: changeFixture}, f.err
}

func (f *fakeCabinet) CorrectFactAt(_ context.Context, hotelID int64, a hotelier.FactAddress, value string, _ map[string]any) (*hotelier.FactOutcome, error) {
	f.addr = a
	return &hotelier.FactOutcome{FactID: 1, Key: a.Key, Action: "correct", Evidence: true, Change: changeFixture}, f.err
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

func TestHotelierPersonaPreview(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, &fakeCabinet{})
	rec := do(h, "POST", "/api/hotelier/questions/7/persona-preview", "")
	var out previewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body)
	}
	if !out.Simulated || out.Persona != "embellishing" || !out.Found || out.ParsedBy != "cache" || out.Text == "" {
		t.Errorf("превью: %+v", out)
	}
	nobody := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, &fakeCabinet{err: hotelier.ErrNoPersona})
	if rec := do(nobody, "POST", "/api/hotelier/questions/7/persona-preview", ""); rec.Code != 409 {
		t.Errorf("без персоны: %d", rec.Code)
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

func TestHotelierFactsByAddress(t *testing.T) {
	svc := &fakeCabinet{}
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, svc)
	body := `{"entity_type":"hotel","entity_id":"5","key":"pets_allowed","value":"true"}`
	rec := do(h, "POST", "/api/hotelier/hotels/5/facts/confirm", body)
	var out factOutcomeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}
	if svc.addr.Key != "pets_allowed" || svc.addr.EntityID != "5" {
		t.Errorf("адрес: %+v", svc.addr)
	}
	if out.Change == nil || out.Change.ConflictChange != conflictOpened || out.Change.Before.Confidence != 0.5 || out.Change.After.Confidence != 0.6 {
		t.Errorf("до и после: %+v", out.Change)
	}
	if rec := do(h, "POST", "/api/hotelier/hotels/5/facts/correct", body); rec.Code != 200 {
		t.Errorf("correct: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/hotelier/hotels/5/facts/correct", `{"entity_type":"hotel","entity_id":"5","key":"k"}`); rec.Code != 400 {
		t.Errorf("correct без value: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/hotelier/hotels/5/facts/confirm", `{}`); rec.Code != 400 {
		t.Errorf("без адреса: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/hotelier/hotels/5/facts/delete", body); rec.Code != 404 {
		t.Errorf("действие: %d", rec.Code)
	}
}

func TestConflictChange(t *testing.T) {
	for _, c := range []struct{ before, after, want string }{
		{"", "", conflictNone}, {"", "спор", conflictOpened}, {"спор", "", conflictClosed}, {"a", "b", conflictRemains},
	} {
		if got := conflictChange(c.before, c.after); got != c.want {
			t.Errorf("%q→%q: %s", c.before, c.after, got)
		}
	}
}

func TestHotelierHotelsAndDemand(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, &fakeCabinet{})
	if rec := do(h, "GET", "/api/hotelier/hotels", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"persona":"embellishing"`) {
		t.Errorf("отели: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/hotelier/5/demand", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"question_id":7`) {
		t.Errorf("спрос: %d %s", rec.Code, rec.Body)
	}
}

func TestHotelierAnswerPassesComment(t *testing.T) {
	svc := &fakeCabinet{}
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, svc)
	if rec := do(h, "POST", "/api/hotelier/questions/7/answer", `{"answer":"no","comment":"леса до марта"}`); rec.Code != 200 {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body)
	}
	if svc.answered != "нет" || svc.meta.Button != "no" || svc.meta.Comment != "леса до марта" {
		t.Errorf("передано: %q %+v", svc.answered, svc.meta)
	}
}

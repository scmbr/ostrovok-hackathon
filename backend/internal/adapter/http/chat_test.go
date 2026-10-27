package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

type fakeChat struct {
	events []dialog.Event
	err    error
	got    dialog.Request
}

func (f *fakeChat) Turn(_ context.Context, req dialog.Request, emit func(dialog.Event)) error {
	f.got = req
	for _, e := range f.events {
		emit(e)
	}
	return f.err
}

func chatRouter(f *fakeChat) http.Handler {
	return newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithChat(f))
}

func TestChatStreamsEventsInOrder(t *testing.T) {
	f := &fakeChat{events: []dialog.Event{
		{Type: dialog.EventStep, Data: dialog.StepEvent{N: 1, Mode: "rule", Decision: "разбираю"}},
		{Type: dialog.EventClarification, Data: dialog.Clarification{Kind: "ambiguity", Question: "Вид или пляж?",
			Options: []dialog.Option{{ID: "sea_view", Label: "вид"}, {ID: "walk_to_beach", Label: "пляж"}}}},
		{Type: dialog.EventResults, Data: dialog.ResultsEvent{Kind: "search", Hotels: []domain.HotelResult{{HotelID: 3, Name: "Arts"}},
			Explanations: []domain.HotelExplanation{{HotelID: 3, Headline: "1. Arts — Standard", Lines: []string{"Тишина: совпало"}}}}},
		{Type: dialog.EventMessage, Data: dialog.MessageEvent{Text: "Нашла Arts.", Source: "template"}},
		{Type: dialog.EventDone, Data: dialog.DoneEvent{SessionID: "s1", Mode: "rule", Asked: 1}},
	}}
	rec := doReq(chatRouter(f), "POST", "/api/chat", `{"session_id":"s1","message":"тихо у моря","choice":"x","hotel_id":7}`)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("статус %d, тип %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	var order []string
	for _, line := range strings.Split(body, "\n") {
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			order = append(order, name)
		}
	}
	if strings.Join(order, ",") != "step,clarification,results,message,done" {
		t.Errorf("порядок событий: %v\n%s", order, body)
	}
	for _, want := range []string{`"question":"Вид или пляж?"`, `"hotel_id":3`, `"headline":"1. Arts — Standard"`, `"source":"template"`, `"clarifications_asked":1`, `"id":"sea_view"`} {
		if !strings.Contains(body, want) {
			t.Errorf("в потоке нет %s:\n%s", want, body)
		}
	}
	if f.got.SessionID != "s1" || f.got.Choice != "x" || f.got.HotelID != 7 || f.got.Message != "тихо у моря" {
		t.Errorf("запрос в сервис: %+v", f.got)
	}
}

func TestChatSimilarResults(t *testing.T) {
	f := &fakeChat{events: []dialog.Event{{Type: dialog.EventResults, Data: dialog.ResultsEvent{Kind: "similar",
		Similar: &dialog.SimilarView{Base: dialog.Shown{ID: 1, Name: "Arts"}, Hotels: []search.SimilarHotel{{Candidate: search.Candidate{ID: 2, Name: "Omm"}}}}}}}}
	body := doReq(chatRouter(f), "POST", "/api/chat", `{"session_id":"s","message":"похожий на этот"}`).Body.String()
	if !strings.Contains(body, `"base":{"id":1,"name":"Arts"}`) || !strings.Contains(body, `"name":"Omm"`) {
		t.Errorf("похожие: %s", body)
	}
}

func TestChatValidation(t *testing.T) {
	long := strings.Repeat("я", maxChatMessage+1)
	cases := map[string]string{
		"без сессии":       `{"message":"привет"}`,
		"пустое сообщение": `{"session_id":"s"}`,
		"длинная сессия":   `{"session_id":"` + strings.Repeat("a", maxSessionID+1) + `","message":"x"}`,
		"длинный текст":    `{"session_id":"s","message":"` + long + `"}`,
		"неизвестное поле": `{"session_id":"s","message":"x","extra":1}`,
		"не JSON":          `привет`,
	}
	for name, body := range cases {
		f := &fakeChat{}
		if rec := doReq(chatRouter(f), "POST", "/api/chat", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: статус %d, ждали 400", name, rec.Code)
		}
	}
}

// Ошибка до первого события — обычный JSON со статусом, после — событие error в потоке.
func TestChatErrors(t *testing.T) {
	rec := doReq(chatRouter(&fakeChat{err: errors.New("БД упала")}), "POST", "/api/chat", `{"session_id":"s","message":"x"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "БД упала") {
		t.Errorf("до потока: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(chatRouter(&fakeChat{err: &search.ValidationError{Msg: "нужен session_id"}}), "POST", "/api/chat", `{"session_id":"s","message":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("ошибка валидации: %d", rec.Code)
	}
	mid := &fakeChat{err: errors.New("сбой"), events: []dialog.Event{{Type: dialog.EventStep, Data: dialog.StepEvent{N: 1}}}}
	rec = doReq(chatRouter(mid), "POST", "/api/chat", `{"session_id":"s","message":"x"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "event: error") {
		t.Errorf("после начала потока ждали событие error: %d %s", rec.Code, rec.Body.String())
	}
}

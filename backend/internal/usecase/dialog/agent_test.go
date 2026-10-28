package dialog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// scriptChat отвечает заранее заданными репликами по порядку.
type scriptChat struct {
	replies []agent.Message
	err     error
	calls   int
}

func (s *scriptChat) Chat(_ context.Context, _ []agent.Message, _ []agent.ToolDef) (agent.Message, error) {
	if s.err != nil {
		return agent.Message{}, s.err
	}
	if s.calls >= len(s.replies) {
		return agent.Message{Role: "assistant", Content: ""}, nil
	}
	m := s.replies[s.calls]
	s.calls++
	return m, nil
}

func call(name, args string) agent.Message {
	return agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "c-" + name, Name: name, Arguments: args}}}
}
func say(text string) agent.Message { return agent.Message{Role: "assistant", Content: text} }

type notCached struct{}

func (notCached) Error() string   { return "нет в кеше" }
func (notCached) NotCached() bool { return true }

func llmRig(t *testing.T, rank func(q domain.Query) []int64, replies ...agent.Message) (*rig, *scriptChat) {
	t.Helper()
	r := newRig(t, rank)
	c := &scriptChat{replies: replies}
	r.e.Chat, r.e.ChatMode = c, ModeLLM
	return r, c
}

func rejection(r *rig) (string, bool) {
	for _, s := range *r.trace {
		if s.Tool == "verify_text" && s.Output["reason"] != nil {
			return s.Output["reason"].(string), true
		}
	}
	return "", false
}

func same(domain.Query) []int64 { return []int64{1, 2, 3} }

// Текст выдачи строит код: запрос и список отелей с ценой. Что бы ни сказала модель (выдуманное число, чужой отель,
// «гарантировано»), в ответ её слова не попадают и переписывать их не просят.
func TestAgentResultsMessageIsShortTemplate(t *testing.T) {
	for _, say1 := range []string{
		"Hotel Arts в 47 минутах от пляжа.",
		"Hotel Arts хорош, но лучше Hotel Santa Marta.",
		"Hotel Arts — тишина там обеспечена, питомцы разрешены.",
		"Нашла Hotel Arts, Hotel Omm и Casa Fuster.",
	} {
		r, c := llmRig(t, same, call("parse_baseline", `{"text":"тихий номер"}`), call("search", `{}`), say(say1))
		ev := turn(t, r, Request{SessionID: "s", Message: "тихий номер"})
		msg, _ := find[MessageEvent](ev, EventMessage)
		if msg.Source != "template" || strings.Contains(msg.Text, say1) {
			t.Errorf("слова модели не должны идти гостю: %+v", msg)
		}
		for _, want := range []string{"Ищу: тишина ночью (желательно).", "Нашла 3 отеля:", "1. Hotel Arts — Стандарт, 100 €", "на карточках"} {
			if !strings.Contains(msg.Text, want) {
				t.Errorf("в ответе нет %q: %s", want, msg.Text)
			}
		}
		for _, banned := range []string{"Лучшие варианты", "уверенность", "за ночь", "гарантир"} {
			if strings.Contains(msg.Text, banned) {
				t.Errorf("в коротком тексте не должно быть %q: %s", banned, msg.Text)
			}
		}
		if c.calls != 3 {
			t.Errorf("вызовов модели %d: просьбы переписать быть не должно", c.calls)
		}
		if _, rejected := rejection(r); rejected {
			t.Error("вводная модели для выдачи не проверяется")
		}
	}
}

// Модель просит search сразу, минуя ask_clarification, — правило уточнения всё равно срабатывает.
func TestAgentCannotBypassClarificationRule(t *testing.T) {
	r, _ := llmRig(t, seaOrBeach, call("search", `{}`), say("Готово."))
	ev := turn(t, r, Request{SessionID: "s", Message: "хочу у моря"})
	if _, ok := find[Clarification](ev, EventClarification); !ok {
		t.Fatalf("ждали уточнение, события: %v", ev)
	}
	if _, ok := find[ResultsEvent](ev, EventResults); ok {
		t.Error("пока гость не ответил, выдачи быть не должно")
	}
	for _, sq := range r.search.queries {
		if !sq.NoDemand {
			t.Errorf("итоговый поиск не должен идти до ответа: %+v", sq)
		}
	}
}

// Через ask_clarification модель получает вопрос только по тому же правилу; вопрос ставит точку в её ходе.
func TestAgentAskClarificationTool(t *testing.T) {
	r, c := llmRig(t, seaOrBeach, call("parse_baseline", `{"text":"у моря"}`), call("ask_clarification", `{"phrase":"у моря"}`), say("не должно быть вызвано"))
	ev := turn(t, r, Request{SessionID: "s", Message: "у моря"})
	q, ok := find[Clarification](ev, EventClarification)
	if !ok || !strings.Contains(q.Question, "пляж") {
		t.Fatalf("ждали вопрос: %v", ev)
	}
	if c.calls != 2 {
		t.Errorf("после вопроса модель больше не вызывается: %d вызовов", c.calls)
	}
	if st := state(t, r, "s"); st.Asked != 1 || st.Pending == nil {
		t.Errorf("состояние: %+v", st)
	}
}

// Модель завершила ход без поиска — результат всё равно считает код.
func TestAgentFallbackSearchWhenModelSkips(t *testing.T) {
	r, _ := llmRig(t, same, say("Добрый день!"))
	ev := turn(t, r, Request{SessionID: "s", Message: "тихий номер"})
	if _, ok := find[ResultsEvent](ev, EventResults); !ok {
		t.Fatal("поиск должен выполнить код")
	}
}

// log_unmet_wish: нормализация и предложение модели записываются; уже понятое по реестру в журнал не идёт.
func TestAgentLogUnmetWish(t *testing.T) {
	r, _ := llmRig(t, same,
		call("parse_baseline", `{"text":"морской воздух"}`),
		call("log_unmet_wish", `{"text":"морской воздух","normalized":"Морской воздух","proposal":{"key":"sea_air"}}`),
		call("log_unmet_wish", `{"text":"тихий номер"}`),
		say("Записала."))
	turn(t, r, Request{SessionID: "s", Message: "морской воздух"})
	if r.unmet["морской воздух"] != 1 || len(r.unmet) != 1 {
		t.Errorf("журнал: %v", r.unmet)
	}
	var refused bool
	for _, s := range *r.trace {
		if s.Tool == "log_unmet_wish" && s.Output["ok"] == false {
			refused = true
		}
	}
	if !refused {
		t.Error("уже понятое желание должно быть отклонено")
	}
}

// Недоступная модель и промах кеша: ход повторяется по правилам, журнал честно пишет mode=rule.
func TestAgentFallsBackToRules(t *testing.T) {
	for name, err := range map[string]error{"сеть": errors.New("timeout"), "кеш": notCached{}} {
		t.Run(name, func(t *testing.T) {
			r, c := llmRig(t, same)
			c.err = err
			ev := turn(t, r, Request{SessionID: "s", Message: "тихий номер и пляж рядом"})
			res, ok := find[ResultsEvent](ev, EventResults)
			if !ok || len(res.Query.Wishes) != 2 {
				t.Fatalf("правила должны довести ход до выдачи: %v", ev)
			}
			msg, _ := find[MessageEvent](ev, EventMessage)
			if msg.Source != "template" {
				t.Errorf("текст шаблонный: %+v", msg)
			}
			modes := map[string]bool{}
			for _, s := range *r.trace {
				modes[s.Mode] = true
			}
			if !modes[ModeLLM] || !modes[ModeRule] {
				t.Errorf("в журнале и попытка модели, и правила: %v", modes)
			}
		})
	}
}

// Пять отказов подряд останавливают модель; ход всё равно завершается поиском.
func TestAgentStopsAfterConsecutiveRejections(t *testing.T) {
	var bad []agent.Message
	for i := 0; i < agent.MaxConsecutiveRejections+3; i++ {
		bad = append(bad, call("nope", `{}`))
	}
	r, c := llmRig(t, same, bad...)
	ev := turn(t, r, Request{SessionID: "s", Message: "тихий номер"})
	if c.calls != agent.MaxConsecutiveRejections {
		t.Errorf("вызовов модели %d, ждали %d", c.calls, agent.MaxConsecutiveRejections)
	}
	if _, ok := find[ResultsEvent](ev, EventResults); !ok {
		t.Error("поиск выполняется кодом")
	}
}

// Модель называет желание и в remove, и в wishes (так делал gpt-4o-mini): желание остаётся, а не стирается.
func TestAgentSearchRemoveThenAdd(t *testing.T) {
	r, _ := llmRig(t, same, call("search", `{"remove":["pets_allowed"],"wishes":[{"key":"pets_allowed","importance":"prefer"}]}`), say("Готово."))
	ev := turn(t, r, Request{SessionID: "s", Message: "с собакой"})
	res, _ := find[ResultsEvent](ev, EventResults)
	if _, ok := has(res.Query, "pets_allowed"); !ok {
		t.Errorf("желание потерялось: %+v", res.Query.Wishes)
	}
}

// Ответ кнопкой применяет код; модель только пересказывает выдачу, а поиск выполняется в любом случае.
func TestAgentChoiceTurn(t *testing.T) {
	r, c := llmRig(t, seaOrBeach, call("parse_baseline", `{"text":"у моря"}`), call("ask_clarification", `{"phrase":"у моря"}`), say("Нашла Hotel Arts."))
	turn(t, r, Request{SessionID: "s", Message: "у моря"})
	before := c.calls
	ev := turn(t, r, Request{SessionID: "s", Choice: "sea_view"})
	if c.calls != before+1 {
		t.Errorf("при ответе кнопкой модель вызывается один раз, вызовов %d", c.calls-before)
	}
	res, ok := find[ResultsEvent](ev, EventResults)
	if !ok || !slices.ContainsFunc(res.Query.Wishes, func(w domain.Desire) bool { return w.Key == "sea_view" }) {
		t.Errorf("выбранная трактовка должна войти в запрос: %v", ev)
	}
}

// Гость просил «похожий»: после similar модель не может подменить ответ обычным поиском.
func TestAgentSearchAfterSimilarRejected(t *testing.T) {
	r, _ := llmRig(t, same, say("Нашла Hotel Arts."), call("similar", `{"ordinal":1}`), call("search", `{}`), say("Готово."))
	turn(t, r, Request{SessionID: "s", Message: "тихий номер"})
	ev := turn(t, r, Request{SessionID: "s", Message: "покажи похожий на первый"})
	res, ok := find[ResultsEvent](ev, EventResults)
	if !ok || res.Kind != "similar" {
		t.Fatalf("ответ должен остаться «похожими»: %+v", res)
	}
}

// Модель вызывает similar, хотя гость «похожих» не просил, — отказ, ответ даёт обычный поиск.
func TestAgentSimilarWithoutRequestRejected(t *testing.T) {
	r, _ := llmRig(t, same, say("Нашла Hotel Arts."), call("similar", `{"current":true}`), say("Готово."))
	turn(t, r, Request{SessionID: "s", Message: "тихий номер"})
	ev := turn(t, r, Request{SessionID: "s", Message: "важно тихо ночью"})
	res, ok := find[ResultsEvent](ev, EventResults)
	if !ok || res.Kind != "search" {
		t.Fatalf("ждали обычную выдачу: %+v", res)
	}
}

package guests

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// Signaler ищет сигналы гостей: несколько надёжных гостей независимо против расчёта в своих ячейках «фасад × этаж».
// Сигнал ставит факты фасадов на пересчёт (stale), заводит задачу пересчёта и пишет причину в agent_trace.
type Signaler struct {
	Registry domain.Registry
	Store    Store
	Rooms    Rooms
	Now      func() time.Time
}

// Signal — найденный сигнал.
type Signal struct {
	HotelID    int64
	HotelName  string
	Key        string
	Votes      []domain.SignalVote
	Facades    []int64
	Categories []int64
	Created    bool // задача создана; false — открытая задача уже была
}

type groupKey struct {
	hotel int64
	key   string
}

// Run просматривает ответы гостей и обрабатывает сигналы.
func (s *Signaler) Run(ctx context.Context, flt domain.HotelFilter) ([]Signal, error) {
	votes, err := s.Store.GuestVotes(ctx, flt)
	if err != nil {
		return nil, fmt.Errorf("ответы гостей: %w", err)
	}
	groups := map[groupKey][]Vote{}
	var order []groupKey
	for _, v := range votes {
		k := groupKey{v.HotelID, v.Key}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], v)
	}
	var out []Signal
	for _, k := range order {
		cells, err := s.Rooms.Cells(ctx, k.hotel)
		if err != nil {
			return out, fmt.Errorf("отель %d: %w", k.hotel, err)
		}
		var sv []domain.SignalVote
		facades, cats := map[int64]bool{}, map[int64]bool{}
		for _, v := range groups[k] {
			calc, ok := cells.ValueAt(v.Key, v.FacadeID, v.Floor)
			if !ok {
				continue
			}
			sv = append(sv, domain.SignalVote{BookingID: v.BookingID, GuestID: v.GuestID, Trust: v.Trust, Value: v.Value, CalcValue: calc})
		}
		if !domain.SignalReached(sv) {
			continue
		}
		against := domain.SignalVotes(sv)
		byBooking := map[int64]bool{}
		for _, a := range against {
			byBooking[a.BookingID] = true
		}
		for _, v := range groups[k] {
			if byBooking[v.BookingID] {
				facades[v.FacadeID] = true
				cats[v.CategoryID] = true
			}
		}
		sig := Signal{HotelID: k.hotel, Key: k.key, Votes: against, Facades: sortedIDs(facades), Categories: sortedIDs(cats)}
		if err := s.handle(ctx, &sig); err != nil {
			return out, err
		}
		out = append(out, sig)
	}
	return out, nil
}

func sortedIDs(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *Signaler) handle(ctx context.Context, sig *Signal) error {
	h, err := s.Store.Hotel(ctx, sig.HotelID)
	if err != nil {
		return err
	}
	sig.HotelName = h.Name
	reason := fmt.Sprintf("%d надёжных гостей (репутация не ниже %.2f) независимо против расчёта в своих ячейках «фасад × этаж»",
		len(sig.Votes), domain.SignalMinTrust)
	votes := make([]map[string]any, len(sig.Votes))
	for i, v := range sig.Votes {
		votes[i] = map[string]any{"booking_id": v.BookingID, "guest_id": v.GuestID, "trust": round2(v.Trust), "answer": v.Value, "calc": v.CalcValue}
	}
	signal := map[string]any{"votes": votes}
	if err := s.Store.MarkStale(ctx, sig.HotelID, sig.Key, sig.Facades); err != nil {
		return fmt.Errorf("пометка stale: %w", err)
	}
	created, err := s.Store.CreateTask(ctx, Task{HotelID: sig.HotelID, Key: sig.Key, Reason: reason, Facades: sig.Facades, Categories: sig.Categories, Signal: signal})
	if err != nil {
		return fmt.Errorf("задача пересчёта: %w", err)
	}
	sig.Created = created
	return s.trace(ctx, sig.HotelID, 1, fmt.Sprintf("сигнал гостей по %s: %s; факты фасадов помечены stale, задача пересчёта %s",
		sig.Key, reason, map[bool]string{true: "создана", false: "уже была"}[created]),
		"guest_signal", map[string]any{"characteristic": sig.Key, "votes": votes},
		map[string]any{"stale_facades": sig.Facades, "categories": sig.Categories, "task_created": created})
}

func (s *Signaler) trace(ctx context.Context, hotelID int64, step int, decision, tool string, in, out map[string]any) error {
	return appendTrace(ctx, s.Store, s.Now, hotelID, step, decision, tool, in, out)
}

func appendTrace(ctx context.Context, st Store, now func() time.Time, hotelID int64, step int, decision, tool string, in, out map[string]any) error {
	t := time.Now()
	if now != nil {
		t = now()
	}
	return st.AppendTrace(ctx, agent.TraceStep{
		Agent: AgentSignal, SessionID: fmt.Sprintf("guests-%d-%d", hotelID, t.Unix()), HotelID: hotelID, Mode: "rule",
		Step: step, Decision: decision, Tool: tool, Input: in, Output: out,
	})
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

// Recomputer выполняет открытые задачи пересчёта: запускает инструмент характеристики по отелю (он пишет только
// свидетельства), движок правды пересобирает факты. Результат сравнивается с прежним значением: пересчёт либо
// подтверждает расчёт (нового здания в данных нет), либо меняет его.
type Recomputer struct {
	Registry domain.Registry
	Store    Store
	Rooms    Rooms
	Runner   agent.ToolRunner
	Now      func() time.Time
}

// Recomputed — итог одной задачи.
type Recomputed struct {
	Task    Task
	Outcome string // confirmed | changed
	Summary string
	Before  map[int64]string
	After   map[int64]string
	// Category — итог движка правды по категориям гостей после пересчёта.
	Category []CategoryOutcome
	Err      error
}

// CategoryOutcome — состояние категории после пересчёта.
type CategoryOutcome struct {
	CategoryID int64
	Value      string
	Confidence float64
	Conflict   string
}

// Исходы пересчёта.
const (
	OutcomeConfirmed = "confirmed"
	OutcomeChanged   = "changed"
)

// Run выполняет открытые задачи.
func (r *Recomputer) Run(ctx context.Context, flt domain.HotelFilter) ([]Recomputed, error) {
	tasks, err := r.Store.OpenTasks(ctx, flt)
	if err != nil {
		return nil, err
	}
	var out []Recomputed
	for _, t := range tasks {
		res, err := r.one(ctx, t)
		out = append(out, res)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (r *Recomputer) one(ctx context.Context, t Task) (Recomputed, error) {
	res := Recomputed{Task: t}
	c, ok := r.Registry.Get(t.Key)
	if !ok || len(c.Strategies) == 0 {
		return res, fmt.Errorf("у характеристики %s нет стратегий расчёта", t.Key)
	}
	h, err := r.Store.Hotel(ctx, t.HotelID)
	if err != nil {
		return res, err
	}
	if res.Before, err = r.Store.FacadeValues(ctx, t.HotelID, t.Key); err != nil {
		return res, err
	}
	var summaries []string
	ran := map[string]bool{}
	for _, st := range c.Strategies {
		if ran[st.Tool] {
			continue
		}
		ran[st.Tool] = true
		sum, err := r.Runner.Run(ctx, h, st.Tool, nil)
		if err != nil {
			res.Err = err
			_ = appendTrace(ctx, r.Store, r.Now, t.HotelID, 2, fmt.Sprintf("пересчёт %s не выполнен: %v", t.Key, err), st.Tool,
				map[string]any{"characteristic": t.Key, "task": t.ID}, map[string]any{"error": err.Error()})
			return res, nil
		}
		summaries = append(summaries, sum)
	}
	res.Summary = fmt.Sprint(summaries)
	if res.After, err = r.Store.FacadeValues(ctx, t.HotelID, t.Key); err != nil {
		return res, err
	}
	res.Outcome = OutcomeChanged
	if reflect.DeepEqual(res.Before, res.After) {
		res.Outcome = OutcomeConfirmed
	}
	for _, id := range t.Categories {
		v, conf, conflict, err := r.Store.CategoryState(ctx, id, t.Key)
		if err != nil {
			return res, err
		}
		res.Category = append(res.Category, CategoryOutcome{id, v, conf, conflict})
	}
	decision := r.decision(res)
	cat := make([]map[string]any, len(res.Category))
	for i, co := range res.Category {
		cat[i] = map[string]any{"category_id": co.CategoryID, "value": co.Value, "confidence": round2(co.Confidence), "conflict": co.Conflict}
	}
	result := map[string]any{"outcome": res.Outcome, "facades_before": stringKeys(res.Before), "facades_after": stringKeys(res.After), "categories": cat, "summary": res.Summary}
	if err := r.Store.CompleteTask(ctx, t.ID, res.Outcome, result); err != nil {
		return res, err
	}
	err = appendTrace(ctx, r.Store, r.Now, t.HotelID, 2, decision, "recompute",
		map[string]any{"characteristic": t.Key, "task": t.ID, "facades": t.Facades}, result)
	return res, err
}

func stringKeys(m map[int64]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[fmt.Sprint(k)] = v
	}
	return out
}

func (r *Recomputer) decision(res Recomputed) string {
	t := res.Task
	conflict := ""
	for _, co := range res.Category {
		if co.Conflict != "" {
			conflict = co.Conflict
		}
	}
	if res.Outcome == OutcomeChanged {
		return fmt.Sprintf("пересчёт %s по свежим данным изменил значения фасадов: категории обновлены по новому расчёту", t.Key)
	}
	if conflict != "" {
		return fmt.Sprintf("пересчёт %s по свежим данным подтвердил расчёт: значения %d фасадов не изменились (нового здания в данных нет). "+
			"Надёжные гости остаются против расчёта, у категории открыт конфликт: %s. Причину (деревья, леса, стройка) знает отельер: вопрос отельеру",
			t.Key, len(res.After), conflict)
	}
	return fmt.Sprintf("пересчёт %s по свежим данным подтвердил расчёт: значения %d фасадов не изменились; конфликта у категории нет", t.Key, len(res.After))
}

package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/guests"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/reputation"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// runGuests — команды по гостям «Гуру»: mock-stays, guest-surveys, guest-signals, recompute-tasks. LLM не нужна:
// ответы персон, анкеты и сигнал — детерминированные правила (docs/guests.md).
func runGuests(ctx context.Context, cmd string, reg domain.Registry, pool *pgxpool.Pool, region domain.Region, flt domain.HotelFilter, cfg Config) error {
	rules := reg.Rules()
	gs := postgres.NewGuestStore(pool, rules)
	roomSvc := &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, rules)}
	switch cmd {
	case "mock-stays":
		res, err := (&guests.Stayer{Registry: reg, Store: gs, Rooms: roomSvc}).Run(ctx, flt)
		if err != nil {
			return err
		}
		fmt.Printf("заселений записано: %d у %d отелей (честных %d, ленивых %d, closed_view %d)\n",
			res.Stays, res.Hotels, res.ByPersona[domain.GuestHonest], res.ByPersona[domain.GuestLazy], res.ByPersona[domain.GuestClosedView])
		if res.ClosedViewHotel != nil {
			fmt.Printf("сценарий мока: %d %s, категория «%s», три гостя сообщают, что вид закрыт, хотя расчёт даёт вид\n",
				res.ClosedViewHotel.ID, res.ClosedViewHotel.Name, res.ClosedViewCat)
		}
	case "guest-surveys":
		sv := &guests.Surveyor{Registry: reg, Store: gs, Rooms: roomSvc,
			Reputation: &reputation.Service{Registry: reg, Store: postgres.NewReputationStore(pool, rules), Guests: gs}}
		res, err := sv.Run(ctx, flt)
		if err != nil {
			return err
		}
		fmt.Printf("анкет проведено: %d (честных %d, ленивых %d, closed_view %d); свидетельств записано %d, ответов вне области %d\n",
			res.Surveys, res.ByPersona[domain.GuestHonest], res.ByPersona[domain.GuestLazy], res.ByPersona[domain.GuestClosedView], res.Evidence, res.OutOfScope)
		fmt.Printf("небрежных анкет: %d, контрольный вопрос провален: %d\n", res.Careless, res.ControlFail)
		byPersona := map[string][]float64{}
		for _, t := range res.Trust {
			byPersona[t.Persona] = append(byPersona[t.Persona], t.After)
		}
		var personas []string
		for p := range byPersona {
			personas = append(personas, p)
		}
		sort.Strings(personas)
		for _, p := range personas {
			ts := byPersona[p]
			fmt.Printf("  репутация %-12s %.2f (гостей %d)\n", p, ts[0], len(ts))
		}
	case "guest-signals":
		sigs, err := (&guests.Signaler{Registry: reg, Store: gs, Rooms: roomSvc}).Run(ctx, flt)
		if err != nil {
			return err
		}
		if len(sigs) == 0 {
			fmt.Println("сигналов гостей нет")
		}
		for _, s := range sigs {
			fmt.Printf("сигнал: %d %s, %s: %d надёжных гостей против расчёта, фасады %v на пересчёт (stale), задача создана: %v\n",
				s.HotelID, s.HotelName, s.Key, len(s.Votes), s.Facades, s.Created)
		}
	case "recompute-tasks":
		runner := newToolRunner(reg, rules, pool, region, cfg.RawDataDir)
		runner.check = postgres.NewAgentStore(pool, rules).RequireSources
		res, err := (&guests.Recomputer{Registry: reg, Store: gs, Rooms: roomSvc, Runner: runner}).Run(ctx, flt)
		if err != nil {
			return err
		}
		if len(res) == 0 {
			fmt.Println("открытых задач пересчёта нет")
		}
		for _, r := range res {
			if r.Err != nil {
				fmt.Printf("задача %d (отель %d, %s): пересчёт не выполнен: %v\n", r.Task.ID, r.Task.HotelID, r.Task.Key, r.Err)
				continue
			}
			fmt.Printf("задача %d (отель %d, %s): %s; фасадов %d\n", r.Task.ID, r.Task.HotelID, r.Task.Key, r.Outcome, len(r.After))
			for _, c := range r.Category {
				fmt.Printf("  категория %d: значение %s, уверенность %.2f, конфликт: %q\n", c.CategoryID, c.Value, c.Confidence, c.Conflict)
			}
		}
	}
	return nil
}

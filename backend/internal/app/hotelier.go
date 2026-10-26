package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// runHotelierRound — collect hotelier-round [-hotel ID] [-answer=false]: раунд агента-интервьюера по отелю
// или по всем демо-отелям. Вопросы выбираются по ценности и бюджету, имитированная персона отвечает (-answer),
// ответ разбирается, всё пишется в agent_trace (agent = hotelier_interviewer).
func runHotelierRound(args []string, reg domain.Registry) error {
	fs := flag.NewFlagSet("hotelier-round", flag.ContinueOnError)
	hotelID := fs.Int64("hotel", 0, "id отеля; без него — все демо-отели")
	regionCode := fs.String("region", defaultRegion, "код региона")
	answer := fs.Bool("answer", true, "имитированная персона сразу отвечает на отправленные вопросы (false — вопросы остаются в дайджесте)")
	noLLM := fs.Bool("no-llm", false, "без LLM: шаблоны вопросов и разбор да / нет")
	cacheOnly := fs.Bool("cache", false, "не обращаться к LLM: ответы только из кеша")
	noRecord := fs.Bool("no-record", false, "не сохранять ответы LLM в кеш")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := LoadConfig()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("миграции: %w", err)
	}
	region, err := postgres.NewRegionStore(pool).LoadRegion(ctx, *regionCode)
	if err != nil {
		return err
	}

	rules := reg.Rules()
	iv := &hotelier.Interviewer{
		Registry: reg,
		Store:    postgres.NewHotelierStore(pool, rules),
		Impact:   &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, rules)},
	}
	var llm *adapterllm.Hotelier
	switch {
	case *noLLM:
		fmt.Println("режим: без LLM (шаблоны и разбор да / нет)")
	case cfg.LLMAPIKey != "" && !*cacheOnly:
		llm, err = adapterllm.NewHotelierLive(adapterllm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel), !*noRecord)
		fmt.Printf("режим: LLM %s\n", cfg.LLMModel)
	default:
		llm, err = adapterllm.NewHotelierCache()
		fmt.Println("LLM_API_KEY не задан: режим кеша, при промахе — шаблоны и разбор да / нет")
	}
	if err != nil {
		return err
	}
	if llm != nil {
		iv.LLM = llm
	}

	ids := []int64{*hotelID}
	if *hotelID == 0 {
		hotels, err := postgres.NewHotelierMockStore(pool).DemoHotels(ctx, domain.HotelFilter{RegionID: region.ID})
		if err != nil {
			return err
		}
		if len(hotels) == 0 {
			return errors.New("нет демо-отелей (collect select-demo)")
		}
		ids = ids[:0]
		for _, h := range hotels {
			ids = append(ids, h.ID)
		}
	}

	var sent, waiting, low, answered, clarify int
	for _, id := range ids {
		res, err := iv.Round(ctx, id, hotelier.Options{Answer: *answer})
		if err != nil {
			return fmt.Errorf("отель %d: %w", id, err)
		}
		sent += len(res.Sent)
		waiting += res.Waiting
		low += res.LowValue
		for _, a := range res.Answers {
			if a.Status == hotelier.StatusAnswered {
				answered++
			} else {
				clarify++
			}
		}
		if *hotelID != 0 || len(res.Candidates) > 0 {
			printHotelierResult(res)
		}
	}
	if len(ids) > 1 {
		fmt.Printf("\nитого по %d отелям: отправлено %d, ждёт бюджета %d, ниже порога %d; ответов разобрано %d, needs_clarification %d\n",
			len(ids), sent, waiting, low, answered, clarify)
	}
	if llm != nil && !*noRecord {
		path, n, err := llm.Save(adapterllm.HotelierCacheDir)
		if err != nil {
			return fmt.Errorf("сохранение кеша: %w", err)
		}
		if n > 0 {
			fmt.Printf("ответов LLM записано в кеш: %d (%s)\n", n, path)
		}
	}
	return nil
}

func printHotelierResult(r hotelier.Result) {
	persona := r.Hotel.Persona
	if persona == "" {
		persona = "живой"
	}
	fmt.Printf("\n%d %s [%s]: кандидатов %d, отправлено %d, ждёт %d, ниже порога %d (бюджет до раунда: %d)\n",
		r.Hotel.ID, r.Hotel.Name, persona, len(r.Candidates), len(r.Sent), r.Waiting, r.LowValue, r.BudgetLeft)
	for _, q := range r.Candidates {
		fmt.Printf("  %-18s %-17s ценность %.2f (спрос %.2f × неуверенность %.2f × влияние %.2f)  %s\n",
			q.Key, q.Status, q.Score.Value, q.Score.DemandNorm, q.Score.Uncertainty, q.Score.Impact, q.Reason)
	}
	for _, q := range r.Sent {
		fmt.Printf("  → %s [%s]: %s\n", q.Key, q.Format, q.Text)
	}
	for _, a := range r.Answers {
		fmt.Printf("  ← %s: «%s» → %s", a.Question.Key, a.Question.AnswerText, a.Status)
		if a.Parsed.Note != "" {
			fmt.Printf(" (%s)", strings.TrimSpace(a.Parsed.Note))
		}
		fmt.Println()
	}
}

package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/openmeteo"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

// toolRunner запускает расчётные инструменты по одному отелю для агента. Инструменты пишут только свидетельства.
type toolRunner struct {
	// check — проверка исходных данных инструмента в этой базе; nil — без проверки.
	check    func(ctx context.Context, tool string) error
	regionID int64
	run      map[string]func(ctx context.Context, opts compute.Options) (string, error)
}

func newToolRunner(reg domain.Registry, rules map[string]domain.TruthParams, pool *pgxpool.Pool, region domain.Region, rawDir string) *toolRunner {
	return &toolRunner{regionID: region.ID, run: map[string]func(context.Context, compute.Options) (string, error){
		"sea-view": func(ctx context.Context, o compute.Options) (string, error) {
			n, err := (&compute.SeaView{Registry: reg, Store: postgres.NewSeaViewStore(pool, rules)}).Run(ctx, o)
			return fmt.Sprintf("sea_view рассчитан, отелей: %d", n), err
		},
		"noise-night": func(ctx context.Context, o compute.Options) (string, error) {
			res, err := (&compute.NoiseNight{Registry: reg, Store: postgres.NewNoiseStore(pool, region.ID, rules)}).Run(ctx, o)
			return fmt.Sprintf("noise_night рассчитан: фасадов %d, с картой BCN %d, только оценка OSM %d, расхождений источников %d",
				res.Facades, res.BCN, res.OSMOnly, res.Conflicts), err
		},
		"walk": func(ctx context.Context, o compute.Options) (string, error) {
			res, err := (&compute.Walk{Registry: reg, Store: postgres.NewWalkStore(pool, rules)}).Run(ctx, o)
			return fmt.Sprintf("маршруты рассчитаны: до пляжа %d, до метро %d, отелей вне графа %d", res.Beach, res.Metro, res.Unsnapped), err
		},
		"night-services": func(ctx context.Context, o compute.Options) (string, error) {
			res, err := (&compute.NightServices{Registry: reg, Store: postgres.NewNightStore(pool, rules)}).Run(ctx, o)
			var parts []string
			for _, k := range res.Keys {
				parts = append(parts, fmt.Sprintf("%s: богато %d, базово %d, ничего %d", k.Key, k.Rich, k.Basic, k.None))
			}
			return "ночные сервисы рассчитаны; " + strings.Join(parts, "; "), err
		},
		"climate": func(ctx context.Context, o compute.Options) (string, error) {
			res, err := (&compute.Climate{
				Registry: reg, Src: openmeteo.NewClient(rawDir), Store: postgres.NewClimateStore(pool, rules), Districts: region.Districts,
			}).Run(ctx, o)
			return fmt.Sprintf("climate_by_month рассчитан у районов: %d", res.Districts), err
		},
	}}
}

// Run запускает инструмент tool по отелю h; параметры — переопределения числовых параметров инструмента.
func (t *toolRunner) Run(ctx context.Context, h agent.HotelInfo, tool string, params map[string]float64) (string, error) {
	fn, ok := t.run[tool]
	if !ok {
		return "", fmt.Errorf("инструмент %q не подключён к агенту", tool)
	}
	if t.check != nil {
		if err := t.check(ctx, tool); err != nil {
			return "", err
		}
	}
	opts := compute.Options{Filter: domain.HotelFilter{HotelID: h.ID}, District: h.District, Params: compute.Params(params)}
	opts.Filter.RegionID = t.regionID
	return fn(ctx, opts)
}

// runAgent — collect agent -hotel ID: прогон агента-сборщика по отелю.
func runAgent(args []string, reg domain.Registry) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	hotelID := fs.Int64("hotel", 0, "id отеля (hotels.id)")
	regionCode := fs.String("region", "barcelona", "код региона")
	planner := fs.Bool("planner", false, "не использовать LLM: детерминированный планировщик")
	cacheOnly := fs.Bool("cache", false, "не обращаться к LLM: воспроизвести прогон из кеша")
	noRecord := fs.Bool("no-record", false, "не сохранять ответы LLM в кеш")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *hotelID <= 0 {
		return errors.New("укажите отель: collect agent -hotel <id>")
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
	runner := newToolRunner(reg, rules, pool, region, cfg.RawDataDir)
	store := postgres.NewAgentStore(pool, rules)
	runner.check = store.RequireSources
	a := &agent.Agent{Registry: reg, Store: store, Runner: runner}

	var rec *adapterllm.Recorder
	switch {
	case *planner:
		fmt.Println("режим: планировщик (без LLM)")
	case cfg.LLMAPIKey != "" && !*cacheOnly:
		live := adapterllm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
		rec = &adapterllm.Recorder{Next: live, Model: cfg.LLMModel}
		a.Chat, a.ChatMode = rec, agent.ModeLLM
		fmt.Printf("режим: LLM %s\n", cfg.LLMModel)
	default:
		replay, err := adapterllm.NewAgentReplay()
		if err != nil {
			return err
		}
		a.Chat, a.ChatMode = replay, agent.ModeCache
		fmt.Printf("LLM_API_KEY не задан: режим кеша (ответов в кеше: %d)\n", replay.Size())
	}

	res, err := a.Run(ctx, *hotelID)
	if err != nil {
		return err
	}
	if rec != nil && !*noRecord {
		path, err := rec.Save(adapterllm.AgentCacheDir, fmt.Sprint(*hotelID))
		if err != nil {
			return fmt.Errorf("сохранение кеша: %w", err)
		}
		fmt.Printf("ответы LLM сохранены: %s\n", path)
	}
	fmt.Printf("прогон %s: режим в конце %s, записей в журнале %d, запусков %d, пропущено свежих %d, вопросов отельеру %d\n",
		res.SessionID, res.Mode, res.Steps, res.Actions, res.Skipped, res.Questions)

	steps, err := store.ListTrace(ctx, *hotelID, res.SessionID)
	if err != nil {
		return err
	}
	for _, s := range steps {
		tool := s.Tool
		if tool == "" {
			tool = "-"
		}
		fmt.Printf("%2d [%s] %-15s %s\n", s.Step, s.Mode, tool, s.Decision)
	}
	return nil
}

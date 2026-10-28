package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// dialogMode — как агент диалога получает ответы модели.
type dialogMode int

const (
	dialogAuto  dialogMode = iota // ключ задан — живая модель, иначе кеш
	dialogCache                   // только кеш, ключ не используется
	dialogRules                   // без модели: парсер и шаблонные тексты
)

// newDialogEngine собирает агента диалога. Живая модель обёрнута в Recorder, если record == true: ответы можно
// сохранить в кеш (cache/dialog). Без ключа ответы берутся из встроенного кеша, на промахе ход идёт по правилам.
func newDialogEngine(cfg Config, reg domain.Registry, pool *pgxpool.Pool, region domain.Region, mode dialogMode, record bool) (*dialog.Engine, *adapterllm.Recorder, string, error) {
	roomSvc := &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, reg.Rules())}
	searchStore := postgres.NewSearchStore(pool)
	searchSvc := &search.Service{Registry: reg, Store: searchStore, Rooms: roomSvc, Demand: searchStore, Poi: searchStore, RegionID: region.ID}
	return newDialogEngineFrom(cfg, reg, pool, region, searchSvc, mode, record)
}

func newDialogEngineFrom(cfg Config, reg domain.Registry, pool *pgxpool.Pool, region domain.Region, searchSvc *search.Service, mode dialogMode, record bool) (*dialog.Engine, *adapterllm.Recorder, string, error) {
	store := postgres.NewDialogStore(pool, dialog.SessionTTL)
	e := &dialog.Engine{
		Registry: reg, Lexicon: domain.NewLexicon(reg), Search: searchSvc, Sessions: store, Unmet: store,
		Trace: postgres.NewAgentStore(pool, reg.Rules()), RegionID: region.ID,
	}
	switch {
	case mode == dialogRules:
		return e, nil, "режим: правила (без LLM)", nil
	case cfg.LLMAPIKey != "" && mode == dialogAuto:
		live := adapterllm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
		e.ChatMode = dialog.ModeLLM
		if record {
			rec := &adapterllm.Recorder{Next: live, Model: cfg.LLMModel}
			e.Chat = rec
			return e, rec, "режим: LLM " + cfg.LLMModel + " (ответы записываются)", nil
		}
		e.Chat = live
		return e, nil, "режим: LLM " + cfg.LLMModel, nil
	default:
		replay, err := adapterllm.NewDialogReplay()
		if err != nil {
			return nil, nil, "", err
		}
		e.Chat, e.ChatMode = replay, dialog.ModeCache
		return e, nil, fmt.Sprintf("режим кеша (ответов в кеше: %d); на промахе — правила и шаблонные тексты", replay.Size()), nil
	}
}

// runDialog — collect dialog [-session ID] [-record ИМЯ] [-cache] [-rules] [-hotel ID] "фраза" "@кнопка" ...:
// проигрывает диалог по ходам и печатает события. «@id» — ответ кнопкой уточнения. С -record ответы живой модели
// сохраняются в cache/dialog/<ИМЯ>.json.
func runDialog(args []string, reg domain.Registry) error {
	fs := flag.NewFlagSet("dialog", flag.ContinueOnError)
	sessionID := fs.String("session", "cli-dialog", "id сессии; состояние сессии перед началом очищается")
	record := fs.String("record", "", "сохранить ответы живой модели в cache/dialog/<имя>.json")
	cacheOnly := fs.Bool("cache", false, "не обращаться к LLM: ответы только из кеша")
	rules := fs.Bool("rules", false, "без модели: детерминированный разбор и шаблонные тексты")
	fixture := fs.String("fixture", "", "демо-диалог из demo_dialogs.json (ходы берутся из фикстуры); запись — в cache/dialog/<имя>.json")
	hotel := fs.Int64("hotel", 0, "открытый отель («похожий на этот»)")
	limit := fs.Int("limit", 0, "сколько отелей в выдаче (по умолчанию 5)")
	explain := fs.Bool("explain", false, "печатать построчное объяснение по каждому отелю выдачи (статусы, значения, источники)")
	regionCode := fs.String("region", defaultRegion, "код региона")
	if err := fs.Parse(args); err != nil {
		return err
	}
	turns := fs.Args()
	if *fixture != "" {
		d, ok := dialog.DemoDialogByName(*fixture)
		if !ok {
			return fmt.Errorf("в demo_dialogs.json нет диалога %q", *fixture)
		}
		turns = d.Turns
		if *record != "" && *record != d.Name {
			return fmt.Errorf("запись фикстуры идёт под её именем %q", d.Name)
		}
	}
	if len(turns) == 0 {
		return fmt.Errorf("нужна хотя бы одна фраза; ответ кнопкой — «@id»")
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
	mode := dialogAuto
	switch {
	case *rules:
		mode = dialogRules
	case *cacheOnly:
		mode = dialogCache
	}
	e, rec, modeText, err := newDialogEngine(cfg, reg, pool, region, mode, *record != "")
	if err != nil {
		return err
	}
	fmt.Println(modeText)
	e.ResultLimit = *limit
	if err := e.Sessions.SaveState(ctx, *sessionID, []byte("{}")); err != nil {
		return err
	}

	for i, t := range turns {
		req := dialog.Request{SessionID: *sessionID, Message: t, HotelID: *hotel}
		if strings.HasPrefix(t, "@") {
			req.Message, req.Choice = "", strings.TrimPrefix(t, "@")
		}
		fmt.Printf("\n--- ход %d: %s\n", i+1, t)
		if err := e.Turn(ctx, req, func(ev dialog.Event) { printDialogEvent(ev, *explain) }); err != nil {
			return err
		}
	}
	if rec != nil && *record != "" {
		path, err := rec.Save(adapterllm.DialogCacheDir, *record)
		if err != nil {
			return fmt.Errorf("сохранение кеша: %w", err)
		}
		fmt.Printf("\nответы LLM сохранены: %s\n", path)
	}
	return nil
}

// printDialogEvent печатает событие ответа кратко: то, что видел бы клиент SSE.
func printDialogEvent(ev dialog.Event, explain bool) {
	switch d := ev.Data.(type) {
	case dialog.StepEvent:
		tool := d.Tool
		if tool == "" {
			tool = "-"
		}
		fmt.Printf("step %d [%s] %s: %s\n", d.N, d.Mode, tool, d.Decision)
	case dialog.Clarification:
		labels := make([]string, len(d.Options))
		for i, o := range d.Options {
			labels[i] = o.ID + "=" + o.Label
		}
		fmt.Printf("clarification: %s [%s]\n", d.Question, strings.Join(labels, " | "))
	case dialog.ResultsEvent:
		var names []string
		if d.Kind == "similar" && d.Similar != nil {
			names = append(names, "похожие на "+d.Similar.Base.Name+":")
			for _, s := range d.Similar.Hotels {
				names = append(names, fmt.Sprintf("%s [%d]", s.Name, s.ID))
			}
		}
		for _, h := range d.Hotels {
			names = append(names, fmt.Sprintf("%s [%d] (%.2f)", h.Name, h.HotelID, h.Best.Score))
		}
		fmt.Printf("results (%s): %s\n", d.Kind, strings.Join(names, "; "))
		if explain {
			for _, x := range d.Explanations {
				fmt.Println(x.Text())
			}
		}
	case dialog.MessageEvent:
		fmt.Printf("message [%s]:\n%s\n", d.Source, d.Text)
	case dialog.DoneEvent:
		b, _ := json.Marshal(d)
		fmt.Printf("done %s\n", b)
	}
}

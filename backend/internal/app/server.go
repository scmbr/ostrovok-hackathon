package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	apphttp "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/http"
	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/admin"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/explain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/lookup"
	usecaseregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/showcase"
)

// runtime — работающее приложение: подключение к БД, действующий реестр и обработчик запросов. Реестр характеристик
// неизменяемый, поэтому «горячая подмена» устроена так: после добавления характеристики граф сервисов собирается заново
// по новому реестру (buildHandler) и обработчик подменяется атомарно. Запросы в полёте дорабатывают на прежнем графе;
// состояние диалога, шортлист и брони лежат в Postgres и подмену переживают.
type runtime struct {
	cfg    Config
	pool   *pgxpool.Pool
	region domain.Region
	custom *postgres.CustomStore
	jobs   *usecaseregistry.Jobs
	auth   *usecaseregistry.Authoring

	reg     atomic.Pointer[domain.Registry]
	handler atomic.Pointer[http.Handler]
}

func newRuntime(ctx context.Context, cfg Config, pool *pgxpool.Pool) (*runtime, error) {
	region, err := postgres.NewRegionStore(pool).LoadRegion(ctx, defaultRegion)
	if err != nil {
		return nil, fmt.Errorf("регион поиска: %w", err)
	}
	rt := &runtime{cfg: cfg, pool: pool, region: region, custom: postgres.NewCustomStore(pool, region.ID), jobs: &usecaseregistry.Jobs{}}
	llm, _, err := newLLM(cfg)
	if err != nil {
		return nil, fmt.Errorf("LLM для реестра: %w", err)
	}
	rt.auth = &usecaseregistry.Authoring{
		LLM: llm, Tools: compute.Specs, Store: rt.custom, Current: rt.Registry, Reload: rt.Reload,
		Parse: adapterregistry.Parse, Marshal: adapterregistry.Marshal,
	}
	if err := rt.Reload(ctx); err != nil {
		return nil, err
	}
	return rt, nil
}

// Registry — действующий реестр.
func (rt *runtime) Registry() domain.Registry { return *rt.reg.Load() }

// ServeHTTP отдаёт запрос действующему обработчику.
func (rt *runtime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*rt.handler.Load()).ServeHTTP(w, r)
}

// Reload перечитывает реестр (встроенный YAML + добавленные текстом), собирает граф сервисов и подменяет обработчик.
// Конфликт ключей и ошибки проверки реестра возвращаются ошибкой: прежний обработчик остаётся.
func (rt *runtime) Reload(ctx context.Context) error {
	rows, err := rt.custom.ListCustom(ctx)
	if err != nil {
		return fmt.Errorf("добавленные характеристики: %w", err)
	}
	reg, err := LoadRegistryWith(rows)
	if err != nil {
		return err
	}
	h, err := rt.buildHandler(reg)
	if err != nil {
		return err
	}
	rt.reg.Store(&reg)
	rt.handler.Store(&h)
	return nil
}

// buildHandler собирает сервисы и маршруты по реестру reg.
func (rt *runtime) buildHandler(reg domain.Registry) (http.Handler, error) {
	cfg, pool, region := rt.cfg, rt.pool, rt.region
	cabinet, err := newServerInterviewer(cfg, reg, pool)
	if err != nil {
		return nil, fmt.Errorf("кабинет отельера: %w", err)
	}
	roomSvc := &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, reg.Rules())}
	searchStore := postgres.NewSearchStore(pool)
	searchSvc := &search.Service{Registry: reg, Store: searchStore, Rooms: roomSvc, Demand: searchStore, RegionID: region.ID}
	chat, _, mode, err := newDialogEngineFrom(cfg, reg, pool, region, searchSvc, dialogAuto, false)
	if err != nil {
		return nil, fmt.Errorf("агент диалога: %w", err)
	}
	log.Println("диалог: " + mode)
	examples, err := dialog.Examples()
	if err != nil {
		return nil, fmt.Errorf("демо-диалоги: %w", err)
	}
	guest := &booking.Service{Registry: reg, Search: searchSvc, Sessions: postgres.NewDialogStore(pool, dialog.SessionTTL), Hotelier: cabinet,
		Demand: searchStore, Bookings: postgres.NewBookingStore(pool), Trace: postgres.NewAgentStore(pool, reg.Rules()), RegionID: region.ID}
	chat.Compare = guest
	explainSvc := &explain.Service{Registry: reg, Store: postgres.NewExplainStore(pool)}
	adminSvc := &admin.Service{Registry: reg, Store: postgres.NewAdminStore(pool), Explain: explainSvc, RegionID: region.ID}
	return apphttp.NewRouter(reg, postgres.NewAgentStore(pool, reg.Rules()), roomSvc, cabinet,
		apphttp.WithSearch(searchSvc), apphttp.WithHotelLookup(&lookup.Service{Store: searchStore, RegionID: region.ID}), apphttp.WithHotelShowcase(&showcase.Service{Store: searchStore, RegionID: region.ID}), apphttp.WithChat(chat), apphttp.WithGuestFlow(guest), apphttp.WithDialogExamples(examples),
		apphttp.WithLLMMode(llmMode(cfg), cfg.LLMModel), apphttp.WithAdmin(adminSvc, explainSvc),
		apphttp.WithRegistryAdmin(registryAdmin{rt: rt})), nil
}

// registryAdmin — порт apphttp.RegistryAdmin: добавление характеристики текстом и расчёт по демо-отелям.
type registryAdmin struct{ rt *runtime }

func (a registryAdmin) Draft(ctx context.Context, text string) (usecaseregistry.Draft, error) {
	return a.rt.auth.Draft(ctx, text)
}

func (a registryAdmin) Save(ctx context.Context, yaml string) (domain.Characteristic, error) {
	return a.rt.auth.Save(ctx, yaml)
}

func (a registryAdmin) Job(id string) (usecaseregistry.JobView, bool) { return a.rt.jobs.Get(id) }

// StartCompute запускает расчёт характеристики key инструментами её стратегий по демо-отелям региона. Инструмент пишет
// только свидетельства, факты собирает движок правды. Универсальный инструмент (night-services) пересчитывает по отелю
// все свои характеристики: значения те же, меняются только даты получения.
func (a registryAdmin) StartCompute(ctx context.Context, key string) (string, error) {
	rt := a.rt
	reg := rt.Registry()
	c, ok := reg.Get(key)
	if !ok {
		return "", fmt.Errorf("нет характеристики %q", key)
	}
	var tools []string
	seen := map[string]bool{}
	for _, s := range c.Strategies {
		if !seen[s.Tool] {
			seen[s.Tool] = true
			tools = append(tools, s.Tool)
		}
	}
	if len(tools) == 0 {
		return "", fmt.Errorf("у %q нет стратегий с инструментами: значение появится из ответа отельера", key)
	}
	hotels, err := postgres.NewHotelierMockStore(rt.pool).DemoHotels(ctx, domain.HotelFilter{RegionID: rt.region.ID})
	if err != nil {
		return "", err
	}
	rules := reg.Rules()
	runner := newToolRunner(reg, rules, rt.pool, rt.region, rt.cfg.RawDataDir)
	runner.check = postgres.NewAgentStore(rt.pool, rules).RequireSources
	id := rt.jobs.Start("Расчёт «"+c.Title+"» по демо-отелям", func(ctx context.Context, p *usecaseregistry.Progress) error {
		total := len(hotels) * len(tools)
		done := 0
		for _, tool := range tools {
			for i, h := range hotels {
				if _, err := runner.Run(ctx, agent.HotelInfo{ID: h.ID, Name: h.Name, Demo: true}, tool, nil); err != nil {
					p.Fail(fmt.Sprintf("%s, отель %d: %v", tool, h.ID, err))
					if i == 0 {
						// Нет исходных данных или инструмент не подключён: по остальным отелям будет то же самое.
						done += len(hotels)
						p.Report(done, total, tool+": пропущен")
						break
					}
				}
				done++
				p.Report(done, total, fmt.Sprintf("%s: отель %d из %d", tool, i+1, len(hotels)))
			}
		}
		return nil
	})
	return id, nil
}

package app

import (
	"context"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/bcnnoise"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/catastro"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/openmeteo"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/overpass"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/collect"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/truth"
)

const collectUsage = `использование: collect <команда> [-region КОД] [-hotel ID] [-district ИМЯ] [-param имя=значение ...]
  -hotel и -param работают у расчётов по отелям, -district — у climate; список параметров и значений по умолчанию — команда tools

команды:
  tools        показать расчётные инструменты: что считают и какие параметры принимают
  osm          выгрузить отели, здания и берег из OSM (Overpass) в БД
  pois         выгрузить бары, клубы, аптеки, кафе и магазины рядом с отелями (после osm)
  climate      рассчитать климат по месяцам для районов (факт climate_by_month, Open-Meteo, нужен интернет)
  walk-data    выгрузить пешеходный граф, метро и пляжи из OSM (после osm; долго, большой запрос)
  night-services  рассчитать ночные сервисы рядом с отелем по opening_hours (факт night_services, после pois)
  walk         рассчитать пешие маршруты до пляжа и метро (факты walk_to_beach, walk_to_metro, после walk-data)
  roads        выгрузить дороги OSM вокруг отелей (после osm)
  facades      рассчитать фасады отелей: стороны здания без глухих стен, выход на улицу или во двор
  sea-view     рассчитать вид на море по фасадам и этажам (факты sea_view, после facades)
  catastro     загрузить здания кадастра с этажностью из data/raw/catastro и привязать отели
  noise        загрузить фасадный шум карты BCN для зданий отелей (после catastro; data/raw/noise)
  noise-night  рассчитать noise_night: карта BCN и оценка OSM, расхождения источников (после facades, noise, pois)
  agent -hotel ID  прогон агента-сборщика по отелю: что считать, когда спросить отельера; журнал в agent_trace (-planner без LLM, -cache из кеша, -no-record)
  hotelier-round [-hotel ID] [-answer=false]  раунд агента-интервьюера отельера по ценности и бюджету; персона отвечает, ответ разбирается (-no-llm, -cache, -no-record)
  hotelier-preview [-hotel ID]  разбор ответа персоны на открытые вопросы без записи в БД (кнопка «ответить как персона»; ответы LLM — в кеш)
  hotelier-reset  снять вопросы, ответы отельеров и репутацию, пересобрать факты (как truth): вернуть отельеров к состоянию до интервью
  llm-check    проверить ключ LLM: один запрос с вызовом инструмента
  add-characteristic "описание"  добавить характеристику по тексту: LLM составляет YAML реестра из существующих инструментов
  truth        пересобрать все факты из активных свидетельств движком правды; печатает конфликты и таблицу по θ и conflict_steps
  mock-cards   сгенерировать мок базовых карточек отелей
  mock-rooms   сгенерировать категории номеров и их размещение у демо-отелей, «Sea View» у береговых (после mock-cards и sea-view; -hotel ID — один отель)
  mock-hotelier  назначить персоны имитированных отельеров демо-отелям, записать мок спроса и скрытую истину (после mock-rooms)
  mock-stays   имитированные гости и брони демо-отелей; у одного отеля три гостя сообщают о закрытом виде (после mock-rooms, пересоздаёт гостей)
  guest-surveys  анкеты гостей по броням: вопросы по ожиданиям и контрольный, свидетельства о категориях, репутация гостей (после mock-stays)
  guest-signals  сигнал гостей: 3 надёжных гостя против расчёта ставят факт на пересчёт (stale), задача и запись в agent_trace
  recompute-tasks  выполнить задачи пересчёта: инструмент по отелю, итог (подтвердил расчёт или изменил) в agent_trace
  rooms        рассчитать значения характеристик в категориях номеров из фактов фасадов и размещения (после mock-rooms; -hotel ID — один отель)
  select-demo  отметить демо-подмножество отелей (флаг hotels.demo)
  stats        показать сводку по собранным данным`

// RunCollect выполняет команду CLI сбора и расчёта.
func RunCollect(args []string) error {
	loadDotEnv()
	if len(args) < 1 {
		return fmt.Errorf("укажите команду\n%s", collectUsage)
	}
	cmd := args[0]
	if !collectCommands[cmd] {
		return fmt.Errorf("неизвестная команда %q\n%s", cmd, collectUsage)
	}
	if cmd == "llm-check" {
		return runLLMCheck()
	}
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	if cmd == "hotelier-reset" {
		return runHotelierReset(args[1:], reg)
	}
	if cmd == "hotelier-preview" {
		return runHotelierPreview(args[1:], reg)
	}
	if cmd == "hotelier-round" {
		return runHotelierRound(args[1:], reg)
	}
	if cmd == "agent" {
		return runAgent(args[1:], reg)
	}
	if cmd == "add-characteristic" {
		return runAddCharacteristic(args[1:], reg)
	}
	flags, err := parseToolFlags(cmd, args[1:])
	if err != nil {
		return err
	}
	if cmd == "tools" {
		printTools(reg)
		return nil
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

	region, err := postgres.NewRegionStore(pool).LoadRegion(ctx, flags.region)
	if err != nil {
		return err
	}
	opts := compute.Options{
		Filter: domain.HotelFilter{RegionID: region.ID, HotelID: flags.hotel}, District: flags.district, Params: flags.params, Sets: flags.sets,
	}

	rules := reg.Rules()
	store := postgres.NewGeoStore(pool, region.ID)
	c := &collect.Collector{
		Region:   region,
		Src:      overpass.NewClient(cfg.RawDataDir),
		Catastro: catastro.NewSource(cfg.RawDataDir),
		Store:    store,
	}

	switch cmd {
	case "osm":
		if err := c.CollectOSM(ctx); err != nil {
			return err
		}
	case "catastro":
		if err := c.CollectCatastro(ctx); err != nil {
			return err
		}
	case "roads":
		if err := c.CollectRoads(ctx); err != nil {
			return err
		}
	case "pois":
		if err := c.CollectPois(ctx); err != nil {
			return err
		}
	case "climate":
		res, err := (&compute.Climate{
			Registry: reg, Src: openmeteo.NewClient(cfg.RawDataDir), Store: postgres.NewClimateStore(pool, rules), Districts: region.Districts,
		}).Run(ctx, opts)
		if err != nil {
			return err
		}
		fmt.Printf("climate_by_month рассчитан у районов: %d; лет данных о море (минимум по районам): %d\n", res.Districts, res.SeaYears)
	case "night-services":
		res, err := (&compute.NightServices{Registry: reg, Store: postgres.NewNightStore(pool, rules)}).Run(ctx, opts)
		if err != nil {
			return err
		}
		for _, k := range res.Keys {
			fmt.Printf("%s: богато %d, базово %d, ничего %d; заведений в радиусе %d, с разобранными часами %d\n",
				k.Key, k.Rich, k.Basic, k.None, k.Pois, k.WithHours)
		}
	case "walk-data":
		if err := c.CollectWalk(ctx); err != nil {
			return err
		}
	case "walk":
		res, err := (&compute.Walk{Registry: reg, Store: postgres.NewWalkStore(pool, rules)}).Run(ctx, opts)
		if err != nil {
			return err
		}
		fmt.Printf("маршруты рассчитаны: отелей на графе %d (вне графа %d), до пляжа %d, до метро %d; целей: пляж %d, метро %d\n",
			res.Hotels, res.Unsnapped, res.Beach, res.Metro, res.BeachTargets, res.MetroTargets)
	case "facades":
		n, err := (&compute.Facades{Store: postgres.NewFacadeStore(pool)}).Run(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("фасады рассчитаны у отелей: %d\n", n)
	case "sea-view":
		sv := postgres.NewSeaViewStore(pool, rules)
		sv.SetAfterHotelSave(roomsRefresh(reg, pool, rules))
		n, err := (&compute.SeaView{Registry: reg, Store: sv}).Run(ctx, opts)
		if err != nil {
			return err
		}
		fmt.Printf("sea_view рассчитан у отелей: %d\n", n)
	case "noise":
		n, err := (&collect.NoiseCollector{Src: bcnnoise.NewSource(cfg.RawDataDir), Store: postgres.NewNoiseStore(pool, region.ID, rules)}).Run(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("сегментов фасадного шума загружено: %d\n", n)
	case "noise-night":
		nn := postgres.NewNoiseStore(pool, region.ID, rules)
		nn.SetAfterHotelSave(roomsRefresh(reg, pool, rules))
		res, err := (&compute.NoiseNight{Registry: reg, Store: nn}).Run(ctx, opts)
		if err != nil {
			return err
		}
		fmt.Printf("noise_night рассчитан у отелей: %d, фактов по фасадам: %d\n", res.Hotels, res.Facades)
		fmt.Printf("  фасадов с картой BCN: %d (источники согласны: %d, расхождения BCN и OSM: %d), только оценка OSM: %d\n",
			res.BCN, res.Same, res.Conflicts, res.OSMOnly)
	case "truth":
		rep, err := (&truth.Recompute{Registry: reg, Store: postgres.NewTruthStore(pool)}).Run(ctx)
		if err != nil {
			return err
		}
		printTruth(rep)
		return nil
	case "mock-cards":
		n, err := c.GenerateMockCards(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("мок-карточек записано: %d\n", n)
	case "mock-rooms":
		res, err := (&collect.RoomsMocker{Store: postgres.NewRoomStore(pool, rules)}).Run(ctx, opts.Filter)
		if err != nil {
			return err
		}
		fmt.Printf("категорий номеров записано: %d у %d отелей, размещений: %d; «Sea View» с видом по расчёту: %d\n",
			res.Categories, res.Hotels, res.Placements, res.SeaViewHonest)
		fmt.Printf("«Sea View» на фасадах без вида (материал для приукрашивающего отельера): %d\n", len(res.Beautifiers))
		for _, name := range res.Beautifiers {
			fmt.Printf("  %s\n", name)
		}
	case "mock-hotelier":
		res, err := (&collect.HotelierMocker{Store: postgres.NewHotelierMockStore(pool)}).Run(ctx, opts.Filter)
		if err != nil {
			return err
		}
		for _, p := range []string{domain.PersonaHonest, domain.PersonaLazy, domain.PersonaEmbellishing} {
			fmt.Printf("%s: %d\n", p, len(res.Personas[p]))
		}
		for _, h := range res.Personas[domain.PersonaEmbellishing] {
			fmt.Printf("  приукрашивающий: %d %s\n", h.ID, h.Name)
		}
		fmt.Printf("спрос (мок) записан по характеристикам: %d\n", res.Demand)
	case "rooms":
		res, err := (&rooms.Projector{Registry: reg, Store: postgres.NewRoomStore(pool, rules)}).Run(ctx, opts.Filter)
		if err != nil {
			return err
		}
		fmt.Printf("категории номеров рассчитаны: отелей %d, категорий %d, свидетельств записано %d\n", res.Hotels, res.Categories, res.Evidence)
	case "mock-stays", "guest-surveys", "guest-signals", "recompute-tasks":
		return runGuests(ctx, cmd, reg, pool, region, opts.Filter, cfg)
	case "select-demo":
		n, err := c.SelectDemo(ctx, collect.DemoSize)
		if err != nil {
			return err
		}
		fmt.Printf("демо-отелей отмечено: %d\n", n)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}
	printStats(stats)
	return nil
}

// defaultRegion — регион, с которым работает collect, если не указан -region.
const defaultRegion = "barcelona"

// collectCommands — известные команды CLI.
var collectCommands = map[string]bool{
	"tools": true, "osm": true, "catastro": true, "roads": true, "pois": true, "walk-data": true, "climate": true,
	"night-services": true, "walk": true, "facades": true, "sea-view": true, "noise": true, "noise-night": true,
	"truth": true, "add-characteristic": true, "llm-check": true, "agent": true, "hotelier-round": true, "hotelier-reset": true, "hotelier-preview": true, "mock-stays": true, "guest-surveys": true, "guest-signals": true, "recompute-tasks": true, "mock-cards": true, "mock-rooms": true, "mock-hotelier": true, "rooms": true,
	"select-demo": true, "stats": true,
}

// roomsCommands — команды по категориям номеров: работают по отелям (-hotel), но не расчётные инструменты реестра.
var roomsCommands = map[string]bool{"mock-rooms": true, "mock-hotelier": true, "rooms": true, "mock-stays": true, "guest-surveys": true, "guest-signals": true, "recompute-tasks": true}

// toolFlags — область и параметры запуска расчётного инструмента.
type toolFlags struct {
	region   string
	hotel    int64
	district string
	params   compute.Params
	sets     map[string][]string
}

// paramFlag собирает повторяющиеся -param имя=значение. Параметр-набор инструмента (kinds) принимает
// значения через запятую: -param kinds=pharmacy,shop.
type paramFlag struct {
	nums    compute.Params
	sets    map[string][]string
	setName map[string]bool
}

func (p *paramFlag) String() string { return fmt.Sprint(map[string]float64(p.nums)) }

func (p *paramFlag) Set(v string) error {
	name, val, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return fmt.Errorf("ожидается имя=значение, получено %q", v)
	}
	if p.setName[name] {
		p.sets[name] = strings.Split(val, ",")
		return nil
	}
	x, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fmt.Errorf("параметр %s: %q не число", name, val)
	}
	p.nums[name] = x
	return nil
}

// parseToolFlags разбирает флаги команды и проверяет, что они применимы к ней.
func parseToolFlags(cmd string, args []string) (toolFlags, error) {
	var f toolFlags
	spec := findSpec(cmd)
	params := &paramFlag{nums: compute.Params{}, sets: map[string][]string{}, setName: map[string]bool{}}
	if spec != nil {
		for _, sp := range spec.SetParams {
			params.setName[sp.Name] = true
		}
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.Int64Var(&f.hotel, "hotel", 0, "считать только этот отель (id из таблицы hotels)")
	fs.StringVar(&f.district, "district", "", "считать только этот район (climate)")
	fs.StringVar(&f.region, "region", defaultRegion, "код региона из таблицы regions")
	fs.Var(params, "param", "параметр инструмента имя=значение, можно повторять")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if fs.NArg() > 0 {
		return f, fmt.Errorf("лишние аргументы: %v", fs.Args())
	}
	f.params, f.sets = params.nums, params.sets
	if len(f.sets) == 0 {
		f.sets = nil
	}
	switch {
	case spec == nil && roomsCommands[cmd] && (f.district != "" || len(params.nums) > 0 || len(params.sets) > 0):
		return f, fmt.Errorf("%s принимает только -hotel и -region", cmd)
	case spec == nil && !roomsCommands[cmd] && (f.hotel != 0 || f.district != "" || len(params.nums) > 0 || len(params.sets) > 0):
		return f, fmt.Errorf("-hotel, -district и -param относятся только к расчётным инструментам (команда tools)")
	case spec != nil && spec.Scope == compute.ScopeHotel && f.district != "":
		return f, fmt.Errorf("%s считает по отелям: используйте -hotel, а не -district", cmd)
	case spec != nil && spec.Scope == compute.ScopeDistrict && f.hotel != 0:
		return f, fmt.Errorf("%s считает по районам: используйте -district, а не -hotel", cmd)
	}
	return f, nil
}

func findSpec(name string) *compute.Spec {
	for i := range compute.Specs {
		if compute.Specs[i].Name == name {
			return &compute.Specs[i]
		}
	}
	return nil
}

// printTools печатает описания инструментов, их параметры со значениями по умолчанию и характеристики из реестра.
func printTools(reg domain.Registry) {
	for _, s := range compute.Specs {
		keys := strings.Join(s.Keys, ", ")
		if s.AnyKey {
			var fromRegistry []string
			for _, u := range reg.ToolUses(s.Name) {
				fromRegistry = append(fromRegistry, u.Characteristic.Key)
			}
			keys = "любые из реестра: " + strings.Join(fromRegistry, ", ")
		}
		fmt.Printf("%s — %s\n  область: %s; характеристики: %s\n", s.Name, s.Description, s.Scope, keys)
		for _, v := range s.Variants {
			fmt.Printf("    strategy %s  (%s) %s\n", v.ID, v.Method, v.Description)
		}
		for _, p := range s.Params {
			fmt.Printf("    -param %s=%g  (%s) %s\n", p.Name, p.Default, p.Unit, p.Description)
		}
		for _, p := range s.SetParams {
			fmt.Printf("    -param %s=%s  %s\n", p.Name, strings.Join(p.Choices, ","), p.Description)
		}
	}
}

func printTruth(r truth.Report) {
	fmt.Printf("факты пересобраны: %d (конфликтов %d, weak_disagreement %d, устаревших %d)\n", r.Facts, r.Conflicts, r.Weak, r.Stale)
	fmt.Printf("noise_night по фасадам: %d фактов, из них с двумя источниками (BCN и OSM): %d\n", r.NoiseFacadeFacts, r.NoiseTwoSources)
	fmt.Println("noise_night, фасады: конфликты / weak_disagreement по θ и conflict_steps (по умолчанию θ=1.0, steps=2)")
	fmt.Printf("  %-6s", "θ")
	for _, steps := range truth.SensitivitySteps {
		fmt.Printf("  steps=%d (конфликт / weak)", steps)
	}
	fmt.Println()
	for _, theta := range truth.SensitivityThetas {
		fmt.Printf("  %-6.1f", theta)
		for _, steps := range truth.SensitivitySteps {
			for _, c := range r.Sensitivity {
				if c.Theta == theta && c.Steps == steps {
					fmt.Printf("  %10d / %-10d", c.Conflicts, c.Weak)
				}
			}
		}
		fmt.Println()
	}
}

func printStats(s domain.Stats) {
	total := 0
	for _, n := range s.HotelsByDistrict {
		total += n
	}
	fmt.Printf("\nОтелей: %d\n", total)
	for _, k := range sortedKeys(s.HotelsByDistrict) {
		fmt.Printf("  %-12s %d\n", k, s.HotelsByDistrict[k])
	}
	fmt.Printf("С привязкой к зданию: %d\n", s.HotelsWithBuilding)
	fmt.Printf("Со зданием, где высота известна (не default): %d\n", s.HotelsWithKnownHeight)
	fmt.Printf("Со зданием кадастра и этажностью: %d\n", s.HotelsWithCatastro)
	fmt.Printf("Со зданием OSM: %d; этажность в двух источниках расходится: %d\n", s.HotelsWithOSMBuilding, s.HotelsHeightDiffers)
	fmt.Printf("Береговых (до %d м от берега): %d, из них в демо: %d\n", domain.CoastalRadiusM, s.CoastalHotels, s.CoastalDemoHotels)
	fmt.Println("Здания по источнику высоты:")
	for _, k := range sortedKeys(s.BuildingsBySource) {
		fmt.Printf("  %-12s %d\n", k, s.BuildingsBySource[k])
	}
	fmt.Printf("Отрезков береговой линии: %d\n", s.SeaSegments)
	fmt.Printf("Демо-отелей: %d\n", s.DemoHotels)
	printFacadeStats(s)
	printSeaViewStats(s)
	printRoomStats(s)
}

func printRoomStats(s domain.Stats) {
	if s.RoomPlacements == 0 {
		fmt.Println("Категории номеров не размещены (collect mock-rooms)")
		return
	}
	fmt.Printf("Категорий номеров: %d, с размещением: %d (размещений %d); названий про море: %d\n",
		s.RoomCategories, s.RoomsPlaced, s.RoomPlacements, s.TitledSeaView)
	fmt.Printf("  название обещает море, а расчёт против (конфликт): %d\n", len(s.SeaViewTitleConflicts))
	for _, t := range s.SeaViewTitleConflicts {
		demo := ""
		if t.Demo {
			demo = " [демо]"
		}
		fmt.Printf("    %s — «%s»%s\n", t.Hotel, t.Category, demo)
	}
}

func printSeaViewStats(s domain.Stats) {
	if s.SeaViewHotels == 0 {
		fmt.Println("sea_view не рассчитан (collect sea-view)")
		return
	}
	fmt.Printf("sea_view: фактов по фасадам %d, по отелям %d\n", s.SeaViewFacadeFacts, s.SeaViewHotels)
	fmt.Printf("  с видом на море: %d отелей (минимальный этаж: 1–2: %d, 3–4: %d, 5 и выше: %d); без вида: %d\n",
		s.SeaViewWithView, s.SeaViewFloors12, s.SeaViewFloors34, s.SeaViewFloors5Plus, s.SeaViewHotels-s.SeaViewWithView)
	fmt.Printf("  береговых отелей (до %d м) без вида: %d\n", domain.CoastalRadiusM, len(s.CoastalNoSeaView))
	for _, name := range s.CoastalNoSeaView {
		fmt.Printf("    %s\n", name)
	}
}

func printFacadeStats(s domain.Stats) {
	fmt.Printf("Дорог в радиусе %d м от отелей: %d\n", domain.RoadsRadiusM, s.Roads)
	if s.Facades == 0 {
		fmt.Println("Фасады не рассчитаны (collect facades)")
		return
	}
	fmt.Printf("Фасадов: %d у %d отелей (на улицу %d, по контуру двора %d)\n",
		s.Facades, s.HotelsFacades, s.StreetFacades, s.CourtyardFacade)
	if s.ContourSegments > 0 {
		fmt.Printf("Отрезков контура: %d, отброшено как глухие стены: %d (%.1f%%)\n",
			s.ContourSegments, s.BlindSegments, 100*float64(s.BlindSegments)/float64(s.ContourSegments))
	}
	fmt.Printf("Отелей без фасада на улицу (после исключения глухих стен): %d\n", len(s.NoStreetFacade))
	for _, h := range s.NoStreetFacade {
		demo := ""
		if h.Demo {
			demo = " [демо]"
		}
		fmt.Printf("  %s (%s), фасадов: %d%s\n", h.Name, h.District, h.Facades, demo)
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// roomsRefresh — реакция на запись фасадных свидетельств: свидетельства категорий номеров отеля пересобираются
// в том же вызове (характеристики без правила category игнорируются).
func roomsRefresh(reg domain.Registry, pool *pgxpool.Pool, rules map[string]domain.TruthParams) postgres.HotelSaved {
	p := &rooms.Projector{Registry: reg, Store: postgres.NewRoomStore(pool, rules)}
	return p.Refresh
}

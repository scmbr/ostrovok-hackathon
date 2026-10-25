package app

import (
	"context"
	"fmt"
	"os/signal"
	"sort"
	"syscall"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/bcnnoise"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/catastro"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/source/overpass"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/collect"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

const collectUsage = `команды:
  osm          выгрузить отели, здания и берег из OSM (Overpass) в БД
  roads        выгрузить дороги OSM вокруг отелей (после osm)
  facades      рассчитать фасады отелей: стороны здания без глухих стен, выход на улицу или во двор
  sea-view     рассчитать вид на море по фасадам и этажам (факты sea_view, после facades)
  catastro     загрузить здания кадастра с этажностью из data/raw/catastro и привязать отели
  noise        загрузить фасадный шум карты BCN для зданий отелей (после catastro; data/raw/noise)
  noise-night  рассчитать noise_night по фасадному шуму (после facades и noise)
  mock-cards   сгенерировать мок базовых карточек отелей
  select-demo  отметить демо-подмножество отелей (флаг hotels.demo)
  stats        показать сводку по собранным данным`

// RunCollect выполняет команду CLI сбора и расчёта.
func RunCollect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("укажите команду\n%s", collectUsage)
	}
	cmd := args[0]
	if cmd != "osm" && cmd != "catastro" && cmd != "roads" && cmd != "facades" && cmd != "sea-view" && cmd != "noise" && cmd != "noise-night" && cmd != "mock-cards" && cmd != "select-demo" && cmd != "stats" {
		return fmt.Errorf("неизвестная команда %q\n%s", cmd, collectUsage)
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

	store := postgres.NewGeoStore(pool)
	c := &collect.Collector{
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
	case "facades":
		n, err := (&compute.Facades{Store: postgres.NewFacadeStore(pool)}).Run(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("фасады рассчитаны у отелей: %d\n", n)
	case "sea-view":
		n, err := (&compute.SeaView{Store: postgres.NewSeaViewStore(pool)}).Run(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("sea_view рассчитан у отелей: %d\n", n)
	case "noise":
		n, err := (&collect.NoiseCollector{Src: bcnnoise.NewSource(cfg.RawDataDir), Store: postgres.NewNoiseStore(pool)}).Run(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("сегментов фасадного шума загружено: %d\n", n)
	case "noise-night":
		res, err := (&compute.NoiseNight{Store: postgres.NewNoiseStore(pool)}).Run(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("noise_night рассчитан у отелей: %d, фактов по фасадам: %d\n", res.Hotels, res.Facades)
	case "mock-cards":
		n, err := c.GenerateMockCards(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("мок-карточек записано: %d\n", n)
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

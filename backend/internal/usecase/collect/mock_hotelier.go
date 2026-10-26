package collect

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Мок отельера: персона, спрос на характеристики и скрытая истина по непроверяемым характеристикам.
// Всё детерминировано от id отеля; в README помечено как мок. Подробности — docs/hotelier.md.

// Ключи характеристик, которые знает только отельер.
const (
	PetsAllowedKey      = "pets_allowed"
	BlackoutCurtainsKey = "blackout_curtains"
)

// DemandSourceMock — источник спроса, пока его не пополняет поиск (этап 6).
const DemandSourceMock = "mock"

// Доли истины мока: питомцы разрешены у ~30 % отелей, затемняющие шторы есть у ~60 %.
const (
	petsAllowedShare      = 0.3
	blackoutCurtainsShare = 0.6
	// honestBuckets — из десяти корзин по хешу id честные первые семь, ленивые остальные.
	honestBuckets = 7
)

// mockDemand — мок спроса: сколько гостей искали характеристику. Море и тишина — главные запросы, ночные аптеки — редкие.
var mockDemand = map[string]int{
	"sea_view": 42, "noise_night": 36, "walk_to_beach": 28, "walk_to_metro": 17, "night_services": 12,
	"night_pharmacy": 6, "climate_by_month": 3, PetsAllowedKey: 14, BlackoutCurtainsKey: 19,
}

// MockDemand возвращает мок спроса по ключам характеристик.
func MockDemand() map[string]int {
	out := make(map[string]int, len(mockDemand))
	for k, v := range mockDemand {
		out[k] = v
	}
	return out
}

// HotelRef — демо-отель для мока отельера.
type HotelRef struct {
	ID   int64
	Name string
}

// TruthRow — скрытая истина мока по отелю и характеристике: {"available": true, ...}.
type TruthRow struct {
	HotelID int64
	Key     string
	Value   map[string]any
}

// MockPersona выбирает персону отельера: приукрашивающий — по расчёту («Sea View» без вида), остальные по хешу id.
func MockPersona(hotelID int64, embellishing bool) string {
	switch {
	case embellishing:
		return domain.PersonaEmbellishing
	case idHash(hotelID)%10 < honestBuckets:
		return domain.PersonaHonest
	default:
		return domain.PersonaLazy
	}
}

// MockTruth строит скрытую истину отеля по характеристикам, которые знает только отельер. Детали питомцев
// (виды, вес, цена) лежат в истине, но в значение характеристики не входят.
func MockTruth(hotelID int64) []TruthRow {
	rng := rand.New(rand.NewSource(hotelID*7919 + 13))
	pets := map[string]any{"available": rng.Float64() < petsAllowedShare}
	// Детали выбираются всегда, чтобы остальные случайные числа не зависели от решения.
	species := [][]string{{"dogs"}, {"dogs", "cats"}, {"cats"}}[rng.Intn(3)]
	maxKg := []int{5, 10, 20}[rng.Intn(3)]
	fee := []int{10, 15, 25}[rng.Intn(3)]
	if pets["available"] == true {
		pets["species"], pets["max_kg"], pets["fee_eur"] = species, maxKg, fee
	}
	curtains := map[string]any{"available": rng.Float64() < blackoutCurtainsShare}
	return []TruthRow{{hotelID, PetsAllowedKey, pets}, {hotelID, BlackoutCurtainsKey, curtains}}
}

// HotelierMockStore — хранилище мока отельера.
type HotelierMockStore interface {
	// DemoHotels — демо-отели региона (по flt.HotelID — один).
	DemoHotels(ctx context.Context, flt domain.HotelFilter) ([]HotelRef, error)
	// EmbellishingHotels — демо-отели, у которых название категории («Sea View») спорит с расчётом вида.
	EmbellishingHotels(ctx context.Context, regionID int64) (map[int64]bool, error)
	SaveHotelierMock(ctx context.Context, regionID int64, personas map[int64]string, truth []TruthRow, demand map[string]int) error
}

// HotelierMocker назначает персоны, пишет спрос и скрытую истину.
type HotelierMocker struct {
	Store HotelierMockStore
}

// HotelierMockResult — итог мока отельера.
type HotelierMockResult struct {
	Personas map[string][]HotelRef // по персонам, отели по id
	Demand   int
}

// Run записывает мок для демо-отелей. Приукрашивающие берутся из открытых конфликтов названия категории с расчётом
// (то, что печатает collect stats), поэтому mock-rooms должен быть выполнен раньше.
func (m *HotelierMocker) Run(ctx context.Context, flt domain.HotelFilter) (HotelierMockResult, error) {
	res := HotelierMockResult{Personas: map[string][]HotelRef{}}
	hotels, err := m.Store.DemoHotels(ctx, flt)
	if err != nil {
		return res, fmt.Errorf("демо-отели: %w", err)
	}
	if len(hotels) == 0 {
		return res, fmt.Errorf("нет демо-отелей (collect select-demo)")
	}
	emb, err := m.Store.EmbellishingHotels(ctx, flt.RegionID)
	if err != nil {
		return res, fmt.Errorf("приукрашивающие отели: %w", err)
	}
	personas := map[int64]string{}
	var truth []TruthRow
	for _, h := range hotels {
		p := MockPersona(h.ID, emb[h.ID])
		personas[h.ID] = p
		res.Personas[p] = append(res.Personas[p], h)
		truth = append(truth, MockTruth(h.ID)...)
	}
	for _, hs := range res.Personas {
		sort.Slice(hs, func(i, j int) bool { return hs[i].ID < hs[j].ID })
	}
	demand := MockDemand()
	res.Demand = len(demand)
	if err := m.Store.SaveHotelierMock(ctx, flt.RegionID, personas, truth, demand); err != nil {
		return res, err
	}
	return res, nil
}

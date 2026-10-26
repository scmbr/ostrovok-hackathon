// Package collect — сценарии сбора исходных данных: OSM-отели, здания, берег и мок карточек.
package collect

import (
	"context"
	"fmt"
	"log"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// bufferDeg — запас вокруг районов для зданий (~300 м): соседние дома влияют на вид из окон.
const bufferDeg = 0.003

// coastBufferDeg — запас для береговой линии (~1 км): море может быть дальше района.
const coastBufferDeg = 0.01

// OSMSource — внешний источник данных OpenStreetMap.
type OSMSource interface {
	Hotels(ctx context.Context, region domain.Region) ([]domain.Hotel, error)
	Buildings(ctx context.Context, bbox domain.BBox) ([]domain.Building, error)
	Coastline(ctx context.Context, bbox domain.BBox) ([]orb.LineString, error)
	Roads(ctx context.Context, bbox domain.BBox) ([]domain.Road, error)
	Pois(ctx context.Context, bbox domain.BBox) ([]domain.Poi, error)
	WalkWays(ctx context.Context, bbox domain.BBox) ([]domain.WalkWay, error)
	Destinations(ctx context.Context, bbox domain.BBox) ([]domain.Destination, error)
}

// CatastroSource — выгрузка зданий кадастра с этажностью.
type CatastroSource interface {
	// Buildings возвращает здания, пересекающие рамку (контуры в EPSG:25831, см. domain.Building).
	Buildings(ctx context.Context, bbox domain.BBox) ([]domain.Building, error)
}

// Store — хранилище собранных данных.
type Store interface {
	UpsertBuildings(ctx context.Context, buildings []domain.Building) error
	UpsertCatastroBuildings(ctx context.Context, buildings []domain.Building) error
	UpsertHotels(ctx context.Context, hotels []domain.Hotel) error
	// LinkHotels (пере)привязывает отели к зданиям OSM и кадастра.
	LinkHotels(ctx context.Context) error
	// ReplaceBuildingParts заменяет части зданий кадастра с разной этажностью.
	ReplaceBuildingParts(ctx context.Context, buildings []domain.Building) error
	// MarkShadowedBuildings помечает здания OSM, дублирующие здания кадастра.
	MarkShadowedBuildings(ctx context.Context) error
	ReplaceSea(ctx context.Context, lines []orb.LineString) error
	// ReplaceRoads заменяет дороги; сохраняются только лежащие не дальше domain.RoadsRadiusM от отелей.
	ReplaceRoads(ctx context.Context, roads []domain.Road) error
	// ReplacePois заменяет POI; сохраняются только лежащие не дальше domain.PoisRadiusM от отелей.
	ReplacePois(ctx context.Context, pois []domain.Poi) error
	// ReplaceWalkEdges заменяет рёбра пешеходного графа.
	ReplaceWalkEdges(ctx context.Context, edges []domain.WalkEdge) error
	// ReplaceDestinations заменяет цели пеших маршрутов.
	ReplaceDestinations(ctx context.Context, dests []domain.Destination) error
	ListHotels(ctx context.Context) ([]domain.HotelKey, error)
	SaveMockCard(ctx context.Context, hotelID int64, card domain.Card) error
	ListDemoCandidates(ctx context.Context) ([]domain.DemoCandidate, error)
	// SetDemo отмечает переданные отели демо, у остальных флаг снимается.
	SetDemo(ctx context.Context, ids []int64) error
	Stats(ctx context.Context) (domain.Stats, error)
}

// Collector выполняет сбор данных.
type Collector struct {
	Region   domain.Region
	Src      OSMSource
	Catastro CatastroSource
	Store    Store
}

// CollectOSM выгружает здания, береговую линию и отели из OSM и сохраняет их.
// Здания грузятся раньше отелей, чтобы отель можно было привязать к зданию.
func (c *Collector) CollectOSM(ctx context.Context) error {
	cover := c.Region.Contour

	buildings, err := c.Src.Buildings(ctx, cover.Pad(bufferDeg))
	if err != nil {
		return fmt.Errorf("здания: %w", err)
	}
	log.Printf("зданий получено: %d", len(buildings))
	if err := c.Store.UpsertBuildings(ctx, buildings); err != nil {
		return fmt.Errorf("сохранение зданий: %w", err)
	}

	coast, err := c.Src.Coastline(ctx, cover.Pad(coastBufferDeg))
	if err != nil {
		return fmt.Errorf("береговая линия: %w", err)
	}
	log.Printf("отрезков береговой линии: %d", len(coast))
	if err := c.Store.ReplaceSea(ctx, coast); err != nil {
		return fmt.Errorf("сохранение берега: %w", err)
	}

	hotels, err := c.Src.Hotels(ctx, c.Region)
	if err != nil {
		return fmt.Errorf("отели: %w", err)
	}
	log.Printf("отелей получено: %d", len(hotels))
	if err := c.Store.UpsertHotels(ctx, hotels); err != nil {
		return fmt.Errorf("сохранение отелей: %w", err)
	}
	return c.Store.LinkHotels(ctx)
}

// CollectCatastro загружает здания кадастра с этажностью в районы охвата и перепривязывает отели.
func (c *Collector) CollectCatastro(ctx context.Context) error {
	buildings, err := c.Catastro.Buildings(ctx, c.Region.Contour.Pad(bufferDeg))
	if err != nil {
		return fmt.Errorf("кадастр: %w", err)
	}
	log.Printf("зданий кадастра получено: %d", len(buildings))
	if err := c.Store.UpsertCatastroBuildings(ctx, buildings); err != nil {
		return fmt.Errorf("сохранение зданий кадастра: %w", err)
	}
	if err := c.Store.ReplaceBuildingParts(ctx, buildings); err != nil {
		return fmt.Errorf("сохранение частей зданий: %w", err)
	}
	if err := c.Store.MarkShadowedBuildings(ctx); err != nil {
		return fmt.Errorf("пометка дубликатов зданий: %w", err)
	}
	return c.Store.LinkHotels(ctx)
}

// GenerateMockCards заполняет hotels.card детерминированным моком и возвращает число отелей.
func (c *Collector) GenerateMockCards(ctx context.Context) (int, error) {
	hotels, err := c.Store.ListHotels(ctx)
	if err != nil {
		return 0, err
	}
	for _, h := range hotels {
		if err := c.Store.SaveMockCard(ctx, h.ID, MockCard(h.OSMID)); err != nil {
			return 0, fmt.Errorf("отель %d: %w", h.ID, err)
		}
	}
	return len(hotels), nil
}

// CollectRoads выгружает дороги OSM и оставляет те, что лежат вокруг отелей (отели должны быть уже загружены).
// walkBufferDeg — запас вокруг районов для пешего графа (~2 км): пляж и метро бывают за пределами района.
const walkBufferDeg = 0.02

// CollectWalk выгружает пешеходный граф и цели маршрутов (метро, пляжи) и сохраняет их.
func (c *Collector) CollectWalk(ctx context.Context) error {
	bbox := c.Region.Contour.Pad(walkBufferDeg)
	ways, err := c.Src.WalkWays(ctx, bbox)
	if err != nil {
		return fmt.Errorf("пешеходные пути: %w", err)
	}
	edges := domain.BuildWalkEdges(ways)
	log.Printf("пешеходных путей %d, рёбер графа %d", len(ways), len(edges))
	dests, err := c.Src.Destinations(ctx, bbox)
	if err != nil {
		return fmt.Errorf("цели маршрутов: %w", err)
	}
	counts := map[string]int{}
	for _, d := range dests {
		counts[d.Kind]++
	}
	log.Printf("целей: метро %d, точек границ пляжей %d", counts[domain.DestMetro], counts[domain.DestBeach])
	if len(edges) == 0 || counts[domain.DestMetro] == 0 || counts[domain.DestBeach] == 0 {
		return fmt.Errorf("выгрузка неполная (рёбер %d, метро %d, пляж %d): БД не изменена",
			len(edges), counts[domain.DestMetro], counts[domain.DestBeach])
	}
	if err := c.Store.ReplaceWalkEdges(ctx, edges); err != nil {
		return fmt.Errorf("сохранение графа: %w", err)
	}
	if err := c.Store.ReplaceDestinations(ctx, dests); err != nil {
		return fmt.Errorf("сохранение целей: %w", err)
	}
	return nil
}

// CollectPois выгружает бары, клубы, аптеки, кафе и магазины рядом с отелями.
func (c *Collector) CollectPois(ctx context.Context) error {
	pois, err := c.Src.Pois(ctx, c.Region.Contour.Pad(bufferDeg))
	if err != nil {
		return fmt.Errorf("POI: %w", err)
	}
	log.Printf("POI получено: %d", len(pois))
	if err := c.Store.ReplacePois(ctx, pois); err != nil {
		return fmt.Errorf("сохранение POI: %w", err)
	}
	return nil
}

func (c *Collector) CollectRoads(ctx context.Context) error {
	roads, err := c.Src.Roads(ctx, c.Region.Contour.Pad(bufferDeg))
	if err != nil {
		return fmt.Errorf("дороги: %w", err)
	}
	log.Printf("дорог получено: %d", len(roads))
	if err := c.Store.ReplaceRoads(ctx, roads); err != nil {
		return fmt.Errorf("сохранение дорог: %w", err)
	}
	return nil
}

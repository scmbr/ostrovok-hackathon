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
	Hotels(ctx context.Context, districts []domain.District) ([]domain.Hotel, error)
	Buildings(ctx context.Context, bbox domain.BBox) ([]domain.Building, error)
	Coastline(ctx context.Context, bbox domain.BBox) ([]orb.LineString, error)
}

// Store — хранилище собранных данных.
type Store interface {
	UpsertBuildings(ctx context.Context, buildings []domain.Building) error
	// UpsertHotels сохраняет отели и привязывает каждый к зданию.
	UpsertHotels(ctx context.Context, hotels []domain.Hotel) error
	ReplaceSea(ctx context.Context, lines []orb.LineString) error
	ListHotels(ctx context.Context) ([]domain.HotelKey, error)
	SaveMockCard(ctx context.Context, hotelID int64, card domain.Card) error
	ListDemoCandidates(ctx context.Context) ([]domain.DemoCandidate, error)
	// SetDemo отмечает переданные отели демо, у остальных флаг снимается.
	SetDemo(ctx context.Context, ids []int64) error
	Stats(ctx context.Context) (domain.Stats, error)
}

// Collector выполняет сбор данных.
type Collector struct {
	Src   OSMSource
	Store Store
}

// CollectOSM выгружает здания, береговую линию и отели из OSM и сохраняет их.
// Здания грузятся раньше отелей, чтобы отель можно было привязать к зданию.
func (c *Collector) CollectOSM(ctx context.Context) error {
	cover := domain.CoverageBBox()

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

	hotels, err := c.Src.Hotels(ctx, domain.Districts)
	if err != nil {
		return fmt.Errorf("отели: %w", err)
	}
	log.Printf("отелей получено: %d", len(hotels))
	if err := c.Store.UpsertHotels(ctx, hotels); err != nil {
		return fmt.Errorf("сохранение отелей: %w", err)
	}
	return nil
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

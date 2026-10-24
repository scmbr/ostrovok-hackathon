// Package compute — расчёт характеристик по собранным геоданным.
package compute

import (
	"context"
	"fmt"
	"log"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// FacadeStore — хранилище для расчёта фасадов; пространственные выборки делает PostGIS.
type FacadeStore interface {
	// HotelBuildings возвращает отели, у которых есть основное здание, с его контуром.
	HotelBuildings(ctx context.Context) ([]domain.HotelBuilding, error)
	// BlindFractions для каждого ребра возвращает долю его длины, лежащую не дальше
	// domain.BlindWallDistM от контуров соседних зданий (кадастра и OSM, кроме самого здания отеля).
	BlindFractions(ctx context.Context, buildingID int64, edges []domain.Edge) ([]float64, error)
	// FacesStreet для каждого луча сообщает, дошёл ли он до дороги раньше, чем упёрся в соседнее здание.
	FacesStreet(ctx context.Context, buildingID int64, probes []domain.Probe) ([]bool, error)
	// ReplaceFacades заменяет фасады отеля и записывает статистику контура.
	ReplaceFacades(ctx context.Context, hotelID int64, facades []domain.Facade, st domain.ContourStats) error
}

// Facades считает фасады отелей.
type Facades struct {
	Store FacadeStore
}

// Run пересчитывает фасады всех отелей со зданием и возвращает число обработанных отелей.
func (c *Facades) Run(ctx context.Context) (int, error) {
	hotels, err := c.Store.HotelBuildings(ctx)
	if err != nil {
		return 0, fmt.Errorf("отели со зданиями: %w", err)
	}
	for i, hb := range hotels {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := c.forHotel(ctx, hb); err != nil {
			return i, fmt.Errorf("отель %d: %w", hb.HotelID, err)
		}
	}
	log.Printf("фасады пересчитаны у %d отелей", len(hotels))
	return len(hotels), nil
}

// forHotel: контур → рёбра → отбрасываем глухие стены → склеиваем в фасады → помечаем уличные.
func (c *Facades) forHotel(ctx context.Context, hb domain.HotelBuilding) error {
	edges := domain.ContourEdges(hb.Geom)
	st := domain.ContourStats{Segments: len(edges)}
	var facades []domain.Facade

	if len(edges) > 0 {
		fractions, err := c.Store.BlindFractions(ctx, hb.BuildingID, edges)
		if err != nil {
			return fmt.Errorf("глухие стены: %w", err)
		}
		if len(fractions) != len(edges) {
			return fmt.Errorf("глухие стены: %d значений на %d рёбер", len(fractions), len(edges))
		}
		blind := make([]bool, len(edges))
		for i, f := range fractions {
			if f >= domain.BlindFractionMin {
				blind[i] = true
				st.Blind++
			}
		}
		facades = domain.BuildFacades(edges, blind)
	}

	if len(facades) > 0 {
		probes := make([]domain.Probe, len(facades))
		for i, f := range facades {
			probes[i] = f.Probe()
		}
		street, err := c.Store.FacesStreet(ctx, hb.BuildingID, probes)
		if err != nil {
			return fmt.Errorf("выход на улицу: %w", err)
		}
		if len(street) != len(facades) {
			return fmt.Errorf("выход на улицу: %d значений на %d фасадов", len(street), len(facades))
		}
		for i := range facades {
			facades[i].HotelID = hb.HotelID
			facades[i].FacesStreet = street[i]
		}
	}
	return c.Store.ReplaceFacades(ctx, hb.HotelID, facades, st)
}

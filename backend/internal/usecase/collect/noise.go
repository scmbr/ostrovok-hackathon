package collect

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// NoiseSource — карта шума по фасадам зданий.
type NoiseSource interface {
	// FacadeSegments возвращает сегменты фасадов зданий с указанными кадастровыми ссылками.
	FacadeSegments(ctx context.Context, refs map[string]bool) ([]domain.NoiseFacadeSegment, error)
}

// NoiseStore — хранилище фасадного шума.
type NoiseStore interface {
	HotelCatastroRefs(ctx context.Context) ([]string, error)
	ReplaceNoiseFacades(ctx context.Context, segs []domain.NoiseFacadeSegment) error
}

// NoiseCollector загружает шум фасадов зданий отелей.
type NoiseCollector struct {
	Src   NoiseSource
	Store NoiseStore
}

// Run загружает сегменты фасадов зданий отелей (кадастр должен быть загружен) и возвращает их число.
func (c *NoiseCollector) Run(ctx context.Context) (int, error) {
	refs, err := c.Store.HotelCatastroRefs(ctx)
	if err != nil {
		return 0, fmt.Errorf("здания отелей: %w", err)
	}
	if len(refs) == 0 {
		return 0, errors.New("у отелей нет зданий кадастра: сначала collect catastro")
	}
	set := make(map[string]bool, len(refs))
	for _, r := range refs {
		set[r] = true
	}
	segs, err := c.Src.FacadeSegments(ctx, set)
	if err != nil {
		return 0, fmt.Errorf("карта шума: %w", err)
	}
	withNoise := map[string]bool{}
	for _, s := range segs {
		withNoise[s.CatastroRef] = true
	}
	log.Printf("шум BCN: зданий отелей %d, из них с фасадным шумом %d", len(refs), len(withNoise))
	if err := c.Store.ReplaceNoiseFacades(ctx, segs); err != nil {
		return 0, fmt.Errorf("сохранение: %w", err)
	}
	return len(segs), nil
}

package compute

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// noiseNightTTL — срок годности: карта 2017 года обновляется раз в пять лет.
const noiseNightTTL = 365 * 24 * time.Hour

// NoiseNightStore — хранилище для расчёта noise_night; сегменты на фасады накладывает PostGIS.
type NoiseNightStore interface {
	// NoiseTargets возвращает отели с фасадами, на которые легли сегменты карты шума.
	NoiseTargets(ctx context.Context) ([]domain.NoiseTarget, error)
	// SaveFacts заменяет активные факты noise_night отеля и его фасадов новыми.
	SaveFacts(ctx context.Context, hotelID int64, facts []domain.Fact) error
}

// NoiseNight считает noise_night по фасадному шуму карты BCN.
type NoiseNight struct {
	Store NoiseNightStore
	Now   func() time.Time // для тестов; по умолчанию time.Now
}

// NoiseNightResult — сколько отелей и фасадов получили факт.
type NoiseNightResult struct {
	Hotels, Facades int
}

// Run пересчитывает noise_night у отелей, для которых нашёлся фасадный шум.
func (c *NoiseNight) Run(ctx context.Context) (NoiseNightResult, error) {
	targets, err := c.Store.NoiseTargets(ctx)
	if err != nil {
		return NoiseNightResult{}, fmt.Errorf("отели с шумом: %w", err)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	var res NoiseNightResult
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		facts := noiseFacts(t, now)
		if len(facts) == 0 {
			continue
		}
		if err := c.Store.SaveFacts(ctx, t.HotelID, facts); err != nil {
			return res, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
		res.Hotels++
		res.Facades += len(facts) - 1
		if (i+1)%50 == 0 {
			log.Printf("noise_night: %d из %d отелей", i+1, len(targets))
		}
	}
	return res, nil
}

type noiseFacade struct {
	facade domain.NoiseTargetFacade
	noise  domain.FacadeNoise
}

// noiseFacts: факт на каждый фасад с шумом и итоговый факт отеля (последним).
func noiseFacts(t domain.NoiseTarget, now time.Time) []domain.Fact {
	var results []noiseFacade
	for _, f := range t.Facades {
		if n, ok := domain.ComputeFacadeNoise(f.Samples); ok {
			results = append(results, noiseFacade{facade: f, noise: n})
		}
	}
	if len(results) == 0 {
		return nil
	}
	until := now.Add(noiseNightTTL)
	var facts []domain.Fact
	quietest, loudest := results[0], results[0]
	for _, r := range results {
		facts = append(facts, noiseFacadeFact(r, now, until))
		if r.noise.LnDB < quietest.noise.LnDB {
			quietest = r
		}
		if r.noise.LnDB > loudest.noise.LnDB {
			loudest = r
		}
	}
	return append(facts, noiseHotelFact(t.HotelID, quietest, loudest, len(results), len(t.Facades), now, until))
}

func noiseFacadeFact(r noiseFacade, now, until time.Time) domain.Fact {
	value := map[string]any{
		"level":        r.noise.Level,
		"ln_db":        round1(r.noise.LnDB),
		"nightlife_db": round1(r.noise.OciLnDB),
	}
	details := map[string]any{
		"facade_azimuth_deg": math.Round(r.facade.AzimuthDeg),
		"faces_street":       r.facade.FacesStreet,
		"courtyard":          r.facade.Courtyard,
		"segments":           r.noise.Segments,
		"segments_ln_db":     segmentLevels(r.facade.Samples),
	}
	return noiseFact(domain.EntityFacade, fmt.Sprint(r.facade.ID), value, details, now, until)
}

func noiseHotelFact(hotelID int64, quietest, loudest noiseFacade, withNoise, facades int, now, until time.Time) domain.Fact {
	value := map[string]any{
		"level":             quietest.noise.Level, // лучший фасад: гость может просить номер с тихой стороны
		"ln_db":             round1(quietest.noise.LnDB),
		"quietest_facade":   quietest.facade.ID,
		"loudest_level":     loudest.noise.Level,
		"loudest_ln_db":     round1(loudest.noise.LnDB),
		"facades_with_data": withNoise,
		"facades_total":     facades,
	}
	details := map[string]any{
		"derived_from":     "факты noise_night по фасадам отеля",
		"quietest_facade":  quietest.facade.ID,
		"loudest_facade":   loudest.facade.ID,
		"spread_db":        round1(loudest.noise.LnDB - quietest.noise.LnDB),
		"level_is_best_of": "фасадов с данными карты шума",
	}
	return noiseFact(domain.EntityHotel, fmt.Sprint(hotelID), value, details, now, until)
}

// noiseFact собирает факт и его единственное свидетельство: расчёт по карте BCN.
func noiseFact(entityType, entityID string, value, details map[string]any, now, until time.Time) domain.Fact {
	details["map"] = map[string]any{
		"name":               "Mapa Estratègic de Soroll de Barcelona, по фасадам",
		"year":               domain.NoiseMapYear,
		"dataset":            "facanes-mapa-estrategic-soroll",
		"license":            "CC BY 4.0, Ajuntament de Barcelona",
		"independence_group": domain.NoiseBCNGroup,
	}
	details["params"] = map[string]any{
		"period":          "ночь (Ln)",
		"quiet_max_db":    domain.NoiseQuietMaxDB,
		"moderate_max_db": domain.NoiseModerateMaxDB,
		"facade_buffer_m": domain.NoiseFacadeBufferM,
		"min_overlap_m":   domain.NoiseMinOverlapM,
		"aggregation":     "энергетическое среднее по сегментам, вес — длина в полосе фасада",
	}
	details["assumptions"] = []string{
		"данные расчётные (модель 2017 года), не измерения",
		"сегменты карты привязаны к фасаду по геометрии: полоса вокруг фасада отеля",
	}
	conf := domain.NoiseBCNReliability
	return domain.Fact{
		EntityType: entityType,
		EntityID:   entityID,
		Key:        domain.NoiseNightKey,
		Value:      value,
		Source:     domain.SourceBCNNoise,
		Method:     domain.NoiseNightBCNMethod,
		Confidence: conf,
		ObservedAt: now,
		ValidUntil: &until,
		Evidence: []domain.Evidence{{
			EntityType: entityType, EntityID: entityID, Key: domain.NoiseNightKey, Claimed: value,
			Source: domain.SourceBCNNoise, Method: domain.NoiseNightBCNMethod,
			AuthorType: domain.AuthorTool, AuthorID: domain.NoiseNightBCNMethod,
			Reliability: conf, ObservedAt: now, ValidUntil: &until, Details: details,
		}},
	}
}

func segmentLevels(ss []domain.NoiseSample) []float64 {
	out := make([]float64, 0, len(ss))
	for _, s := range ss {
		out = append(out, round1(s.LnDB))
	}
	return out
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

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

// NoiseNightStore — хранилище для расчёта noise_night; сегменты и источники на фасады накладывает PostGIS.
type NoiseNightStore interface {
	// NoiseTargets возвращает отели с фасадами: сегменты карты шума (если есть) и источники шума OSM вокруг.
	NoiseTargets(ctx context.Context) ([]domain.NoiseTarget, error)
	// SaveFacts заменяет активные факты, свидетельства и конфликты noise_night отеля и его фасадов новыми.
	SaveFacts(ctx context.Context, hotelID int64, facts []domain.Fact) error
}

// NoiseNight считает noise_night: карта шума BCN по фасадам (если покрывает фасад) и независимая оценка по OSM.
type NoiseNight struct {
	Store NoiseNightStore
	Now   func() time.Time // для тестов; по умолчанию time.Now
}

// NoiseNightResult — сколько отелей и фасадов получили факт и как согласуются источники.
type NoiseNightResult struct {
	Hotels, Facades int
	BCN, OSMOnly    int // фасадов с картой BCN и только с оценкой OSM
	Conflicts       int // фасадов, где источники разошлись на ступень и более
	Same            int // фасадов, где источники дали одну ступень
}

// Run пересчитывает noise_night у всех отелей с фасадами.
func (c *NoiseNight) Run(ctx context.Context) (NoiseNightResult, error) {
	targets, err := c.Store.NoiseTargets(ctx)
	if err != nil {
		return NoiseNightResult{}, fmt.Errorf("отели с источниками шума: %w", err)
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
		facts, st := noiseFacts(t, now)
		if len(facts) == 0 {
			continue
		}
		if err := c.Store.SaveFacts(ctx, t.HotelID, facts); err != nil {
			return res, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
		res.Hotels++
		res.Facades += len(facts) - 1
		res.BCN += st.bcn
		res.OSMOnly += st.osmOnly
		res.Conflicts += st.conflicts
		res.Same += st.same
		if (i+1)%50 == 0 {
			log.Printf("noise_night: %d из %d отелей", i+1, len(targets))
		}
	}
	return res, nil
}

type noiseStats struct{ bcn, osmOnly, conflicts, same int }

// noiseFacade — итог по фасаду: оба источника (BCN может отсутствовать) и значение, которое лежит в факте.
type noiseFacade struct {
	facade domain.NoiseTargetFacade
	bcn    *domain.FacadeNoise
	osm    domain.OSMNoise
	lnDB   float64
	level  string
}

// noiseFacts: факт на каждый фасад и итоговый факт отеля (последним). Значение факта берётся из BCN, если карта
// покрывает фасад, иначе из оценки OSM. При расхождении источников на ступень и более факт получает конфликт.
func noiseFacts(t domain.NoiseTarget, now time.Time) ([]domain.Fact, noiseStats) {
	var st noiseStats
	var results []noiseFacade
	for _, f := range t.Facades {
		r := noiseFacade{facade: f, osm: domain.EstimateOSMNoise(f.OSM)}
		r.lnDB, r.level = r.osm.LnDB, r.osm.Level
		if n, ok := domain.ComputeFacadeNoise(f.Samples); ok {
			r.bcn = &n
			r.lnDB, r.level = n.LnDB, n.Level
			st.bcn++
			if domain.NoiseConflict(n.Level, r.osm.Level) {
				st.conflicts++
			} else {
				st.same++
			}
		} else {
			st.osmOnly++
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return nil, st
	}
	until := now.Add(noiseNightTTL)
	var facts []domain.Fact
	quietest, loudest := results[0], results[0]
	for _, r := range results {
		facts = append(facts, noiseFacadeFact(r, now, until))
		if r.lnDB < quietest.lnDB {
			quietest = r
		}
		if r.lnDB > loudest.lnDB {
			loudest = r
		}
	}
	return append(facts, noiseHotelFact(t.HotelID, quietest, loudest, len(results), now, until)), st
}

func noiseFacadeFact(r noiseFacade, now, until time.Time) domain.Fact {
	value := map[string]any{"level": r.level, "ln_db": round1(r.lnDB), "primary_source": r.primarySource()}
	if r.bcn != nil {
		value["nightlife_db"] = round1(r.bcn.OciLnDB)
	}
	id := fmt.Sprint(r.facade.ID)
	f := noiseFact(domain.EntityFacade, id, r, value, r.evidence(domain.EntityFacade, id, now, until), now, until)
	if r.bcn != nil && domain.NoiseConflict(r.bcn.Level, r.osm.Level) {
		f.Conflict = &domain.Conflict{Description: fmt.Sprintf(
			"карта шума BCN: %s (Ln %.1f дБ); оценка по OSM: %s (Ln %.1f дБ, главный источник %s)",
			r.bcn.Level, r.bcn.LnDB, r.osm.Level, r.osm.LnDB, r.osm.Dominant)}
	}
	return f
}

func noiseHotelFact(hotelID int64, quietest, loudest noiseFacade, facades int, now, until time.Time) domain.Fact {
	value := map[string]any{
		"level":           quietest.level, // лучший фасад: гость может просить номер с тихой стороны
		"ln_db":           round1(quietest.lnDB),
		"primary_source":  quietest.primarySource(),
		"quietest_facade": quietest.facade.ID,
		"loudest_level":   loudest.level,
		"loudest_ln_db":   round1(loudest.lnDB),
		"facades_total":   facades,
	}
	id := fmt.Sprint(hotelID)
	evs := quietest.evidence(domain.EntityHotel, id, now, until)
	for i := range evs {
		evs[i].Details["derived_from"] = "свидетельства лучшего (самого тихого) фасада"
		evs[i].Details["quietest_facade"] = quietest.facade.ID
		evs[i].Details["loudest_facade"] = loudest.facade.ID
		evs[i].Details["spread_db"] = round1(loudest.lnDB - quietest.lnDB)
	}
	return noiseFact(domain.EntityHotel, id, quietest, value, evs, now, until)
}

func (r noiseFacade) primarySource() string {
	if r.bcn != nil {
		return domain.SourceBCNNoise
	}
	return domain.SourceOSM
}

// evidence: свидетельство карты BCN (если есть) и оценка OSM; у каждого свой набор деталей.
func (r noiseFacade) evidence(entityType, entityID string, now, until time.Time) []domain.Evidence {
	var out []domain.Evidence
	if r.bcn != nil {
		value := map[string]any{"level": r.bcn.Level, "ln_db": round1(r.bcn.LnDB), "nightlife_db": round1(r.bcn.OciLnDB)}
		out = append(out, noiseEvidence(entityType, entityID, value, domain.SourceBCNNoise, domain.NoiseNightBCNMethod,
			domain.NoiseBCNReliability, now, until, map[string]any{
				"facade_azimuth_deg": math.Round(r.facade.AzimuthDeg),
				"faces_street":       r.facade.FacesStreet,
				"courtyard":          r.facade.Courtyard,
				"segments":           r.bcn.Segments,
				"segments_ln_db":     segmentLevels(r.facade.Samples),
				"independence_group": domain.NoiseBCNGroup,
				"map": map[string]any{
					"name":    "Mapa Estratègic de Soroll de Barcelona, по фасадам",
					"year":    domain.NoiseMapYear,
					"dataset": "facanes-mapa-estrategic-soroll",
					"license": "CC BY 4.0, Ajuntament de Barcelona",
				},
				"params": map[string]any{
					"period": "ночь (Ln)", "facade_buffer_m": domain.NoiseFacadeBufferM, "min_overlap_m": domain.NoiseMinOverlapM,
					"aggregation": "энергетическое среднее по сегментам, вес — длина в полосе фасада",
				},
				"assumptions": []string{
					"данные расчётные (модель 2017 года), не измерения",
					"сегменты карты привязаны к фасаду по геометрии: полоса вокруг фасада отеля",
				},
			}))
	}
	value := map[string]any{"level": r.osm.Level, "ln_db": round1(r.osm.LnDB), "dominant": r.osm.Dominant}
	out = append(out, noiseEvidence(entityType, entityID, value, domain.SourceOSM, domain.NoiseNightOSMMethod,
		domain.NoiseOSMReliability, now, until, map[string]any{
			"facade_azimuth_deg": math.Round(r.facade.AzimuthDeg),
			"roads":              r.osm.Roads,
			"nightlife":          r.osm.Nightlife,
			"exposed_sources":    r.osm.Exposed,
			"independence_group": domain.NoiseOSMGroup,
			"params": map[string]any{
				"radius_m": domain.NoiseOSMRadiusM, "background_db": domain.NoiseBackgroundDB,
				"shield_db": domain.NoiseShieldDB, "exposed_half_deg": domain.NoiseExposedHalfDeg,
			},
			"assumptions": []string{
				"эвристика: уровень по классу дороги и виду заведения на расстоянии 10 м, затухание с расстоянием",
				"источник за зданием (вне ±75° от нормали фасада) ослаблен на 20 дБ",
				"интенсивность движения и часы работы заведений не учитываются",
			},
		}))
	return out
}

func noiseEvidence(entityType, entityID string, value map[string]any, source, method string, reliability float64,
	now, until time.Time, details map[string]any) domain.Evidence {
	return domain.Evidence{
		EntityType: entityType, EntityID: entityID, Key: domain.NoiseNightKey, Claimed: value,
		Source: source, Method: method, AuthorType: domain.AuthorTool, AuthorID: method,
		Reliability: reliability, ObservedAt: now, ValidUntil: &until, Details: details,
	}
}

// noiseFact собирает факт из свидетельств; значение и уверенность — по главному источнику (BCN, иначе OSM).
func noiseFact(entityType, entityID string, r noiseFacade, value map[string]any, evs []domain.Evidence, now, until time.Time) domain.Fact {
	source, method, conf := domain.SourceOSM, domain.NoiseNightOSMMethod, domain.NoiseOSMReliability
	if r.bcn != nil {
		source, method, conf = domain.SourceBCNNoise, domain.NoiseNightBCNMethod, domain.NoiseBCNReliability
	}
	return domain.Fact{
		EntityType: entityType, EntityID: entityID, Key: domain.NoiseNightKey, Value: value,
		Source: source, Method: method, Confidence: conf, ObservedAt: now, ValidUntil: &until, Evidence: evs,
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

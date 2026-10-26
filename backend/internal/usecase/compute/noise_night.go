package compute

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// NoiseNightStore — хранилище для расчёта noise_night; сегменты и источники на фасады накладывает PostGIS.
type NoiseNightStore interface {
	// NoiseTargets возвращает отели с фасадами: сегменты карты шума (если есть) и источники шума OSM вокруг.
	NoiseTargets(ctx context.Context, flt domain.HotelFilter, search domain.NoiseSearch) ([]domain.NoiseTarget, error)
	EvidenceSaver
}

// Варианты инструмента noise-night.
const (
	NoiseVariantBCN = "bcn"
	NoiseVariantOSM = "osm"
)

// NoiseNightSpec — описание инструмента noise_night.
var NoiseNightSpec = Spec{
	Name:        "noise-night",
	Description: "ночной шум по фасадам: карта шума BCN (если покрывает фасад) и независимая оценка по дорогам и заведениям OSM",
	Keys:        []string{domain.NoiseNightKey},
	Scope:       ScopeHotel,
	Variants: []Variant{
		{NoiseVariantBCN, domain.NoiseNightBCNMethod, "карта шума BCN по фасадам"},
		{NoiseVariantOSM, domain.NoiseNightOSMMethod, "оценка по дорогам и заведениям OSM"},
	},
	Params: []Param{
		{"osm_radius_m", "м", "радиус учёта дорог и ночных заведений OSM вокруг фасада", domain.NoiseOSMRadiusM},
		{"facade_buffer_m", "м", "полоса вокруг фасада, в которую попадают сегменты карты BCN", domain.NoiseFacadeBufferM},
		{"min_overlap_m", "м", "минимальная длина сегмента карты в полосе фасада (или половина сегмента, если он короче)", domain.NoiseMinOverlapM},
	},
}

// NoiseNight считает noise_night: карта шума BCN по фасадам (если покрывает фасад) и независимая оценка по OSM.
type NoiseNight struct {
	Registry domain.Registry
	Store    NoiseNightStore
	Now      func() time.Time // для тестов; по умолчанию time.Now
}

// NoiseNightResult — сколько отелей и фасадов получили факт и как согласуются источники.
type NoiseNightResult struct {
	Hotels, Facades int
	BCN, OSMOnly    int // фасадов с картой BCN и только с оценкой OSM
	Conflicts       int // фасадов, где источники разошлись на ступень и более
	Same            int // фасадов, где источники дали одну ступень
}

// Run пересчитывает noise_night у всех отелей с фасадами.
func (c *NoiseNight) Run(ctx context.Context, opts Options) (NoiseNightResult, error) {
	p, err := NoiseNightSpec.ResolveLayers(registryParams(c.Registry, NoiseNightSpec.Name), opts.Params)
	if err != nil {
		return NoiseNightResult{}, err
	}
	bcn, err := strategyCfg(c.Registry, domain.NoiseNightKey, NoiseNightSpec.Name, NoiseVariantBCN)
	if err != nil {
		return NoiseNightResult{}, err
	}
	osm, err := strategyCfg(c.Registry, domain.NoiseNightKey, NoiseNightSpec.Name, NoiseVariantOSM)
	if err != nil {
		return NoiseNightResult{}, err
	}
	cfg := noiseCfg{bcn: bcn, osm: osm}
	search := domain.NoiseSearch{OSMRadiusM: p["osm_radius_m"], FacadeBufferM: p["facade_buffer_m"], MinOverlapM: p["min_overlap_m"]}
	targets, err := c.Store.NoiseTargets(ctx, opts.Filter, search)
	if err != nil {
		return NoiseNightResult{}, fmt.Errorf("отели с источниками шума: %w", err)
	}
	if len(targets) == 0 && opts.Filter.HotelID != 0 {
		return NoiseNightResult{}, fmt.Errorf("отель %d не найден или у него нет фасадов", opts.Filter.HotelID)
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
		evs, st := noiseEvidences(t, search, now, cfg)
		if len(evs) == 0 {
			continue
		}
		if err := c.Store.SaveHotelEvidence(ctx, t.HotelID, domain.NoiseNightKey, noiseAuthors, evs); err != nil {
			return res, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
		res.Hotels++
		res.Facades += len(t.Facades)
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
	cfg    noiseCfg
	facade domain.NoiseTargetFacade
	search domain.NoiseSearch
	bcn    *domain.FacadeNoise
	osm    domain.OSMNoise
	lnDB   float64
	level  string
}

// noiseCfg — стратегии реестра двух источников noise_night.
type noiseCfg struct{ bcn, osm StrategyCfg }

// noiseAuthors — инструменты, чьи свидетельства noise_night пересчитываются вместе.
var noiseAuthors = []domain.Author{domain.ToolAuthor(domain.NoiseNightBCNMethod), domain.ToolAuthor(domain.NoiseNightOSMMethod)}

// noiseEvidences: свидетельства каждого фасада и лучшего (самого тихого) фасада от имени отеля.
// Факты и конфликты из них собирает domain.AssembleFacts: значение берётся из BCN, если карта покрывает фасад,
// иначе из оценки OSM; расхождение источников на ступень и более — конфликт фасада.
func noiseEvidences(t domain.NoiseTarget, search domain.NoiseSearch, now time.Time, cfg noiseCfg) ([]domain.Evidence, noiseStats) {
	var st noiseStats
	var results []noiseFacade
	for _, f := range t.Facades {
		r := noiseFacade{cfg: cfg, facade: f, search: search, osm: domain.EstimateOSMNoise(f.OSM)}
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
	var evs []domain.Evidence
	quietest, loudest := results[0], results[0]
	for _, r := range results {
		evs = append(evs, r.evidence(domain.EntityFacade, fmt.Sprint(r.facade.ID), now)...)
		if r.lnDB < quietest.lnDB {
			quietest = r
		}
		if r.lnDB > loudest.lnDB {
			loudest = r
		}
	}
	return append(evs, noiseHotelEvidence(t.HotelID, quietest, loudest, len(results), now)...), st
}

// noiseHotelEvidence — свидетельства лучшего фасада от имени отеля: гость может просить номер с тихой стороны.
// Сводка по отелю лежит в claimed, и значение факта отеля равно ей.
func noiseHotelEvidence(hotelID int64, quietest, loudest noiseFacade, facades int, now time.Time) []domain.Evidence {
	evs := quietest.evidence(domain.EntityHotel, fmt.Sprint(hotelID), now)
	for i := range evs {
		evs[i].Claimed["quietest_facade"] = quietest.facade.ID
		evs[i].Claimed["loudest_level"] = loudest.level
		evs[i].Claimed["loudest_ln_db"] = round1(loudest.lnDB)
		evs[i].Claimed["facades_total"] = facades
		evs[i].Details["derived_from"] = "свидетельства лучшего (самого тихого) фасада"
		evs[i].Details["quietest_facade"] = quietest.facade.ID
		evs[i].Details["loudest_facade"] = loudest.facade.ID
		evs[i].Details["spread_db"] = round1(loudest.lnDB - quietest.lnDB)
	}
	return evs
}

// evidence: свидетельство карты BCN (если есть) и оценка OSM; у каждого свой набор деталей.
func (r noiseFacade) evidence(entityType, entityID string, now time.Time) []domain.Evidence {
	var out []domain.Evidence
	if r.bcn != nil {
		value := map[string]any{"level": r.bcn.Level, "ln_db": round1(r.bcn.LnDB), "nightlife_db": round1(r.bcn.OciLnDB)}
		out = append(out, noiseEvidence(entityType, entityID, value, domain.SourceBCNNoise, domain.NoiseNightBCNMethod,
			r.cfg.bcn, now, r.cfg.bcn.WithGroup(map[string]any{
				"facade_azimuth_deg": math.Round(r.facade.AzimuthDeg),
				"faces_street":       r.facade.FacesStreet,
				"courtyard":          r.facade.Courtyard,
				"segments":           r.bcn.Segments,
				"segments_ln_db":     segmentLevels(r.facade.Samples),
				"map": map[string]any{
					"name":    "Mapa Estratègic de Soroll de Barcelona, по фасадам",
					"year":    domain.NoiseMapYear,
					"dataset": "facanes-mapa-estrategic-soroll",
					"license": "CC BY 4.0, Ajuntament de Barcelona",
				},
				"params": map[string]any{
					"period": "ночь (Ln)", "facade_buffer_m": r.search.FacadeBufferM, "min_overlap_m": r.search.MinOverlapM,
					"aggregation": "энергетическое среднее по сегментам, вес — длина в полосе фасада",
				},
				"assumptions": []string{
					"данные расчётные (модель 2017 года), не измерения",
					"сегменты карты привязаны к фасаду по геометрии: полоса вокруг фасада отеля",
				},
			})))
	}
	value := map[string]any{"level": r.osm.Level, "ln_db": round1(r.osm.LnDB), "dominant": r.osm.Dominant}
	out = append(out, noiseEvidence(entityType, entityID, value, domain.SourceOSM, domain.NoiseNightOSMMethod,
		r.cfg.osm, now, r.cfg.osm.WithGroup(map[string]any{
			"facade_azimuth_deg": math.Round(r.facade.AzimuthDeg),
			"roads":              r.osm.Roads,
			"nightlife":          r.osm.Nightlife,
			"exposed_sources":    r.osm.Exposed,
			"params": map[string]any{
				"radius_m": r.search.OSMRadiusM, "background_db": domain.NoiseBackgroundDB,
				"shield_db": domain.NoiseShieldDB, "exposed_half_deg": domain.NoiseExposedHalfDeg,
			},
			"assumptions": []string{
				"эвристика: уровень по классу дороги и виду заведения на расстоянии 10 м, затухание с расстоянием",
				"источник за зданием (вне ±75° от нормали фасада) ослаблен на 20 дБ",
				"интенсивность движения и часы работы заведений не учитываются",
			},
		})))
	return out
}

func noiseEvidence(entityType, entityID string, value map[string]any, source, method string, cfg StrategyCfg,
	now time.Time, details map[string]any) domain.Evidence {
	until := cfg.Until(now)
	return domain.Evidence{
		EntityType: entityType, EntityID: entityID, Key: domain.NoiseNightKey, Claimed: value,
		Source: source, Method: method, AuthorType: domain.AuthorTool, AuthorID: method,
		Reliability: cfg.Reliability, ObservedAt: now, ValidUntil: &until, Details: details,
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

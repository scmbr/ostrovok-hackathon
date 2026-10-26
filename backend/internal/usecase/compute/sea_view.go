package compute

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// SeaViewStore — хранилище для расчёта sea_view; лучи и пересечения считает PostGIS.
type SeaViewStore interface {
	// SeaViewTargets возвращает отели с фасадами, основным зданием и этажностью.
	SeaViewTargets(ctx context.Context, flt domain.HotelFilter) ([]domain.SeaViewTarget, error)
	// SeaRays для каждого запроса возвращает луч с расстоянием до берега (суша → вода)
	// и зданиями на пути до берега, в том же порядке. Здания-дубликаты (shadowed) не возвращаются.
	SeaRays(ctx context.Context, rays []domain.RayQuery, rayMaxM float64) ([]domain.SeaRay, error)
	EvidenceSaver
}

// SeaViewSpec — описание инструмента sea_view.
var SeaViewSpec = Spec{
	Name:        "sea-view",
	Description: "вид на море по фасадам и этажам: лучи веера от фасада до береговой линии, помехи — здания кадастра и OSM",
	Keys:        []string{domain.SeaViewKey},
	Scope:       ScopeHotel,
	Params: []Param{
		{"fan_half_deg", "°", "половина веера лучей от нормали фасада", domain.SeaFanHalfDeg},
		{"fan_step_deg", "°", "шаг между лучами веера", domain.SeaFanStepDeg},
		{"ray_max_m", "м", "дальше этого расстояния берег не ищем", domain.SeaRayMaxM},
		{"min_fraction", "доля", "доля лучей веера, с которой этаж считается имеющим вид на море", domain.SeaViewMinFraction},
	},
}

// SeaView считает характеристику sea_view: с какого этажа фасад видит море.
type SeaView struct {
	Registry domain.Registry
	Store    SeaViewStore
	Now      func() time.Time // для тестов; по умолчанию time.Now
}

// Run пересчитывает sea_view у отелей с фасадами (по opts.Filter) и возвращает их число.
func (c *SeaView) Run(ctx context.Context, opts Options) (int, error) {
	p, err := SeaViewSpec.ResolveLayers(registryParams(c.Registry, SeaViewSpec.Name), opts.Params)
	if err != nil {
		return 0, err
	}
	cfg, err := strategyCfg(c.Registry, domain.SeaViewKey, SeaViewSpec.Name, "")
	if err != nil {
		return 0, err
	}
	targets, err := c.Store.SeaViewTargets(ctx, opts.Filter)
	if err != nil {
		return 0, fmt.Errorf("отели с фасадами: %w", err)
	}
	if len(targets) == 0 && opts.Filter.HotelID != 0 {
		return 0, fmt.Errorf("отель %d не найден или у него нет фасадов", opts.Filter.HotelID)
	}
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := c.forHotel(ctx, t, p, cfg); err != nil {
			return i, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
		if (i+1)%25 == 0 {
			log.Printf("sea_view: %d из %d отелей", i+1, len(targets))
		}
	}
	return len(targets), nil
}

func (c *SeaView) forHotel(ctx context.Context, t domain.SeaViewTarget, p Params, cfg StrategyCfg) error {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	var queries []domain.RayQuery
	for _, f := range t.Facades {
		origin := domain.RayOrigin(f.Mid, f.AzimuthDeg)
		for _, az := range domain.FanAzimuthsWith(f.AzimuthDeg, p["fan_half_deg"], p["fan_step_deg"]) {
			queries = append(queries, domain.RayQuery{Origin: origin, AzimuthDeg: az})
		}
	}
	rays, err := c.Store.SeaRays(ctx, queries, p["ray_max_m"])
	if err != nil {
		return fmt.Errorf("лучи: %w", err)
	}
	if len(rays) != len(queries) {
		return fmt.Errorf("лучи: %d значений на %d запросов", len(rays), len(queries))
	}

	perFacade := len(queries) / max(len(t.Facades), 1)
	var evs []domain.Evidence
	var best *facadeResult
	withView := 0
	maxLevels := 0
	for _, f := range t.Facades {
		maxLevels = max(maxLevels, f.Levels)
	}
	results := make([]facadeResult, len(t.Facades))
	for i, f := range t.Facades {
		v := domain.ComputeSeaViewWith(rays[i*perFacade:(i+1)*perFacade], f.Levels, p["min_fraction"])
		results[i] = facadeResult{facade: f, view: v}
		evs = append(evs, facadeEvidence(f, v, p, now, cfg))
		if v.MinFloor > 0 {
			withView++
			// Фасад низкой части (подиум под башней) номеров, скорее всего, не имеет: лучшим его не берём.
			if f.Levels*2 >= maxLevels && (best == nil || better(results[i], *best)) {
				best = &results[i]
			}
		}
	}
	evs = append(evs, hotelEvidence(t, best, withView, now, cfg))
	return c.Store.SaveHotelEvidence(ctx, t.HotelID, domain.SeaViewKey, []domain.Author{domain.ToolAuthor(domain.SeaViewMethod)}, evs)
}

type facadeResult struct {
	facade domain.StoredFacade
	view   domain.SeaView
}

// better: ниже минимальный этаж, при равенстве — шире обзор на этом этаже.
func better(a, b facadeResult) bool {
	if a.view.MinFloor != b.view.MinFloor {
		return a.view.MinFloor < b.view.MinFloor
	}
	return a.view.Fractions[a.view.MinFloor-1] > b.view.Fractions[b.view.MinFloor-1]
}

func facadeEvidence(f domain.StoredFacade, v domain.SeaView, p Params, now time.Time, cfg StrategyCfg) domain.Evidence {
	fractions := make([]float64, len(v.Fractions))
	for i, x := range v.Fractions {
		fractions[i] = round2(x)
	}
	value := map[string]any{
		"min_floor":     nilIfZero(v.MinFloor),
		"levels":        v.Levels,
		"view_fraction": fractions, // по этажам 1..levels
	}
	if v.NearestWaterM >= 0 {
		value["nearest_water_m"] = math.Round(v.NearestWaterM)
	}
	details := map[string]any{
		"facade_azimuth_deg": math.Round(f.AzimuthDeg),
		"rays":               map[string]any{"total": v.Rays, "with_water": v.RaysWithWater},
		"blockers":           blockersEvidence(v.Blockers),
		"params": map[string]any{
			"fan_half_deg": p["fan_half_deg"], "fan_step_deg": p["fan_step_deg"],
			"ray_max_m": p["ray_max_m"], "floor_height_m": domain.FloorHeightM,
			"window_height_m": domain.WindowHeightM, "offshore_m": domain.SeaOffshoreM,
			"min_fraction": p["min_fraction"],
		},
		"inputs": map[string]any{
			"buildings": "кадастр (этажи × 3 м) и здания OSM, не перекрытые кадастром",
			"coastline": "OSM natural=coastline",
		},
		"assumptions": []string{
			"рельеф плоский (DEM не используется)",
			"морем считается вода за береговой линией OSM, включая воду портов, марин и молов",
			"деревья и надстройки не учитываются",
		},
	}
	return seaViewEvidence(domain.EntityFacade, fmt.Sprint(f.ID), value, v.Confidence(), now, details, cfg)
}

func hotelEvidence(t domain.SeaViewTarget, best *facadeResult, withView int, now time.Time, cfg StrategyCfg) domain.Evidence {
	levels := 0
	for _, f := range t.Facades {
		levels = max(levels, f.Levels)
	}
	value := map[string]any{
		"min_floor":         nil,
		"levels":            levels,
		"facades_total":     len(t.Facades),
		"facades_with_view": withView,
	}
	conf := cfg.Reliability
	details := map[string]any{"derived_from": "факты sea_view по фасадам отеля"}
	if best != nil {
		value["min_floor"] = best.view.MinFloor
		value["levels"] = best.view.Levels
		value["facade_id"] = best.facade.ID
		value["azimuth_deg"] = math.Round(best.facade.AzimuthDeg)
		conf = best.view.Confidence()
		details["facade_id"] = best.facade.ID
		details["blockers"] = blockersEvidence(best.view.Blockers)
	}
	return seaViewEvidence(domain.EntityHotel, fmt.Sprint(t.HotelID), value, conf, now, details, cfg)
}

// seaViewEvidence — свидетельство расчёта: надёжность равна уверенности расчёта по качеству высот, но не выше
// базы стратегии из реестра (потолок).
func seaViewEvidence(entityType, entityID string, value map[string]any, reliability float64, now time.Time, details map[string]any, cfg StrategyCfg) domain.Evidence {
	reliability = math.Min(reliability, cfg.Reliability)
	until := cfg.Until(now)
	cfg.WithGroup(details)
	return domain.Evidence{
		EntityType: entityType, EntityID: entityID, Key: domain.SeaViewKey, Claimed: value,
		Source: domain.SourceComputed, Method: domain.SeaViewMethod,
		AuthorType: domain.AuthorTool, AuthorID: domain.SeaViewMethod,
		Reliability: reliability, ObservedAt: now, ValidUntil: &until, Details: details,
	}
}

func blockersEvidence(bs []domain.Obstacle) []map[string]any {
	out := make([]map[string]any, 0, len(bs))
	for _, b := range bs {
		out = append(out, map[string]any{
			"building_id": b.BuildingID, "height_m": round2(b.HeightM),
			"height_source": b.HeightSource, "dist_m": math.Round(b.DistM),
		})
	}
	return out
}

func nilIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

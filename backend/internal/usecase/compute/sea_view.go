package compute

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// seaViewTTL — срок годности факта: застройка и высоты меняются медленно.
const seaViewTTL = 365 * 24 * time.Hour

// SeaViewStore — хранилище для расчёта sea_view; лучи и пересечения считает PostGIS.
type SeaViewStore interface {
	// SeaViewTargets возвращает отели с фасадами, основным зданием и этажностью.
	SeaViewTargets(ctx context.Context) ([]domain.SeaViewTarget, error)
	// SeaRays для каждого запроса возвращает луч с расстоянием до берега (суша → вода)
	// и зданиями на пути до берега, в том же порядке. Здания-дубликаты (shadowed) не возвращаются.
	SeaRays(ctx context.Context, rays []domain.RayQuery) ([]domain.SeaRay, error)
	// SaveFacts заменяет активные факты sea_view отеля и его фасадов новыми.
	SaveFacts(ctx context.Context, hotelID int64, facts []domain.Fact) error
}

// SeaView считает характеристику sea_view: с какого этажа фасад видит море.
type SeaView struct {
	Store SeaViewStore
	Now   func() time.Time // для тестов; по умолчанию time.Now
}

// Run пересчитывает sea_view у всех отелей с фасадами и возвращает их число.
func (c *SeaView) Run(ctx context.Context) (int, error) {
	targets, err := c.Store.SeaViewTargets(ctx)
	if err != nil {
		return 0, fmt.Errorf("отели с фасадами: %w", err)
	}
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := c.forHotel(ctx, t); err != nil {
			return i, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
		if (i+1)%25 == 0 {
			log.Printf("sea_view: %d из %d отелей", i+1, len(targets))
		}
	}
	return len(targets), nil
}

func (c *SeaView) forHotel(ctx context.Context, t domain.SeaViewTarget) error {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	var queries []domain.RayQuery
	for _, f := range t.Facades {
		origin := domain.RayOrigin(f.Mid, f.AzimuthDeg)
		for _, az := range domain.FanAzimuths(f.AzimuthDeg) {
			queries = append(queries, domain.RayQuery{Origin: origin, AzimuthDeg: az})
		}
	}
	rays, err := c.Store.SeaRays(ctx, queries)
	if err != nil {
		return fmt.Errorf("лучи: %w", err)
	}
	if len(rays) != len(queries) {
		return fmt.Errorf("лучи: %d значений на %d запросов", len(rays), len(queries))
	}

	perFacade := len(queries) / max(len(t.Facades), 1)
	var facts []domain.Fact
	var best *facadeResult
	withView := 0
	maxLevels := 0
	for _, f := range t.Facades {
		maxLevels = max(maxLevels, f.Levels)
	}
	results := make([]facadeResult, len(t.Facades))
	for i, f := range t.Facades {
		v := domain.ComputeSeaView(rays[i*perFacade:(i+1)*perFacade], f.Levels)
		results[i] = facadeResult{facade: f, view: v}
		facts = append(facts, facadeFact(f, v, now))
		if v.MinFloor > 0 {
			withView++
			// Фасад низкой части (подиум под башней) номеров, скорее всего, не имеет: лучшим его не берём.
			if f.Levels*2 >= maxLevels && (best == nil || better(results[i], *best)) {
				best = &results[i]
			}
		}
	}
	facts = append(facts, hotelFact(t, best, withView, now))
	return c.Store.SaveFacts(ctx, t.HotelID, facts)
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

func facadeFact(f domain.StoredFacade, v domain.SeaView, now time.Time) domain.Fact {
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
	until := now.Add(seaViewTTL)
	return domain.Fact{
		EntityType: domain.EntityFacade,
		EntityID:   fmt.Sprint(f.ID),
		Key:        domain.SeaViewKey,
		Value:      value,
		Source:     domain.SourceComputed,
		Method:     domain.SeaViewMethod,
		Confidence: v.Confidence(),
		ObservedAt: now,
		ValidUntil: &until,
		Evidence: map[string]any{
			"facade_azimuth_deg": math.Round(f.AzimuthDeg),
			"rays":               map[string]any{"total": v.Rays, "with_water": v.RaysWithWater},
			"blockers":           blockersEvidence(v.Blockers),
			"params": map[string]any{
				"fan_half_deg": domain.SeaFanHalfDeg, "fan_step_deg": domain.SeaFanStepDeg,
				"ray_max_m": domain.SeaRayMaxM, "floor_height_m": domain.FloorHeightM,
				"window_height_m": domain.WindowHeightM, "offshore_m": domain.SeaOffshoreM,
				"min_fraction": domain.SeaViewMinFraction,
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
		},
	}
}

func hotelFact(t domain.SeaViewTarget, best *facadeResult, withView int, now time.Time) domain.Fact {
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
	conf := 0.85
	evidence := map[string]any{"derived_from": "факты sea_view по фасадам отеля"}
	if best != nil {
		value["min_floor"] = best.view.MinFloor
		value["levels"] = best.view.Levels
		value["facade_id"] = best.facade.ID
		value["azimuth_deg"] = math.Round(best.facade.AzimuthDeg)
		conf = best.view.Confidence()
		evidence["facade_id"] = best.facade.ID
		evidence["blockers"] = blockersEvidence(best.view.Blockers)
	}
	until := now.Add(seaViewTTL)
	return domain.Fact{
		EntityType: domain.EntityHotel,
		EntityID:   fmt.Sprint(t.HotelID),
		Key:        domain.SeaViewKey,
		Value:      value,
		Source:     domain.SourceComputed,
		Method:     domain.SeaViewMethod,
		Confidence: conf,
		ObservedAt: now,
		ValidUntil: &until,
		Evidence:   evidence,
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

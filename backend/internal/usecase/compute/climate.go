package compute

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// climateTTL — срок годности: месячные нормы за десять лет меняются медленно.
const climateTTL = 365 * 24 * time.Hour

// ClimateSource — суточная погода и температура моря для точки.
type ClimateSource interface {
	Daily(ctx context.Context, p orb.Point, from, to time.Time) ([]domain.ClimateDay, error)
}

// ClimateStore — запись фактов по районам.
type ClimateStore interface {
	// SaveDistrictFacts заменяет активные факты характеристики key у района новыми.
	SaveDistrictFacts(ctx context.Context, district, key string, facts []domain.Fact) error
}

// Climate считает climate_by_month по районам: месячные нормы воздуха, осадков, солнца и температуры моря.
type Climate struct {
	Src       ClimateSource
	Store     ClimateStore
	Districts []domain.District
	Now       func() time.Time // для тестов; по умолчанию time.Now
}

// ClimateResult — сколько районов получили факт и сколько лет данных моря есть.
type ClimateResult struct {
	Districts int
	SeaYears  int // минимум по районам
}

// Run загружает последние ClimateYears полных лет по центру каждого района и пишет факт района.
func (c *Climate) Run(ctx context.Context) (ClimateResult, error) {
	res := ClimateResult{SeaYears: math.MaxInt}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	toYear := now.Year() - 1
	from := time.Date(toYear-domain.ClimateYears+1, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(toYear, 12, 31, 0, 0, 0, 0, time.UTC)
	for _, d := range c.Districts {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		centre := orb.Point{(d.BBox.West + d.BBox.East) / 2, (d.BBox.South + d.BBox.North) / 2}
		days, err := c.Src.Daily(ctx, centre, from, to)
		if err != nil {
			return res, fmt.Errorf("район %s: %w", d.Name, err)
		}
		norms := domain.AggregateClimate(days)
		if norms.Days == 0 {
			return res, fmt.Errorf("район %s: нет данных за %d–%d", d.Name, from.Year(), to.Year())
		}
		fact := climateFact(d, centre, norms, from.Year(), to.Year(), now)
		if err := c.Store.SaveDistrictFacts(ctx, d.Name, domain.ClimateKey, []domain.Fact{fact}); err != nil {
			return res, fmt.Errorf("район %s: %w", d.Name, err)
		}
		res.Districts++
		res.SeaYears = min(res.SeaYears, norms.SeaYears)
	}
	if res.Districts == 0 {
		res.SeaYears = 0
	}
	return res, nil
}

func climateFact(d domain.District, centre orb.Point, n domain.ClimateNorms, fromYear, toYear int, now time.Time) domain.Fact {
	until := now.Add(climateTTL)
	months := make([]map[string]any, 0, 12)
	for _, m := range n.Months {
		row := map[string]any{
			"month": m.Month, "air_mean_c": round1(m.AirMeanC), "air_max_c": round1(m.AirMaxC), "air_min_c": round1(m.AirMinC),
			"precip_mm": math.Round(m.PrecipMM), "rain_days": round1(m.RainDays), "sun_hours_per_day": round1(m.SunHours),
			"water_c": nil, "swimming": m.Swimming,
		}
		if m.WaterC != nil {
			row["water_c"] = round1(*m.WaterC)
		}
		months = append(months, row)
	}
	value := map[string]any{
		"months":          months,
		"swimming_months": n.SwimmingMonths(),
		"period":          map[string]any{"from_year": fromYear, "to_year": toYear},
	}
	details := map[string]any{
		"centre":              map[string]any{"lon": round5v(centre[0]), "lat": round5v(centre[1])},
		"days_with_data":      n.Days,
		"sea_years_with_data": n.SeaYears,
		"independence_group":  domain.ClimateGroup,
		"datasets": map[string]any{
			"weather": "Open-Meteo Historical Weather API (реанализ ERA5), https://open-meteo.com",
			"sea":     "Open-Meteo Marine API, sea_surface_temperature",
			"license": "CC BY 4.0, атрибуция Open-Meteo.com",
		},
		"params": map[string]any{
			"rain_day_mm": domain.RainDayMM, "swimming_min_water_c": domain.SwimmingMinWaterC,
			"aggregation": "температура и солнце — среднее по суткам; осадки и дождливые дни — среднее по годам за месяц; вода — среднее по имеющимся суткам",
		},
		"assumptions": []string{
			"ячейка реанализа 9–25 км: значения общие для района, микроклимат улицы не учитывается",
			"температура моря есть не за все годы периода (см. sea_years_with_data), месяцы без данных — null",
			"купание = вода не холоднее 20 °C; ветер, волны и медузы не учитываются",
		},
	}
	id := d.Name
	return domain.Fact{
		EntityType: domain.EntityDistrict, EntityID: id, Key: domain.ClimateKey, Value: value,
		Source: domain.SourceOpenMeteo, Method: domain.ClimateMethod, Confidence: domain.ClimateReliability,
		ObservedAt: now, ValidUntil: &until,
		Evidence: []domain.Evidence{{
			EntityType: domain.EntityDistrict, EntityID: id, Key: domain.ClimateKey, Claimed: value,
			Source: domain.SourceOpenMeteo, Method: domain.ClimateMethod, AuthorType: domain.AuthorTool, AuthorID: domain.ClimateMethod,
			Reliability: domain.ClimateReliability, ObservedAt: now, ValidUntil: &until, Details: details,
		}},
	}
}

func round5v(x float64) float64 { return math.Round(x*1e5) / 1e5 }

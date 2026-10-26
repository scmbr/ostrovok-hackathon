package compute

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// nightServicesTTL — срок годности: заведения открываются и закрываются, часы меняются.
const nightServicesTTL = 90 * 24 * time.Hour

// maxNightEvidencePois — сколько открытых ночью заведений показывается в evidence.
const maxNightEvidencePois = 15

// NightServicesStore — хранилище для расчёта night_services.
type NightServicesStore interface {
	// NightTargets возвращает отели с заведениями в радиусе radiusM.
	NightTargets(ctx context.Context, flt domain.HotelFilter, radiusM float64) ([]domain.NightTarget, error)
	EvidenceSaver
}

// NightServicesSpec — описание инструмента night_services.
var NightServicesSpec = Spec{
	Name:        "night-services",
	Description: "что открыто ночью рядом с отелем: аптеки, магазины, еда и напитки по opening_hours заведений OSM",
	Keys:        []string{domain.NightServicesKey},
	Scope:       ScopeHotel,
	Params: []Param{
		{"radius_m", "м", "заведения дальше этого расстояния по прямой от отеля не учитываются", domain.NightRadiusM},
	},
}

// NightServices считает night_services: что открыто ночью рядом с отелем по opening_hours из OSM.
type NightServices struct {
	Store NightServicesStore
	Now   func() time.Time // для тестов; по умолчанию time.Now
}

// NightServicesResult — сколько отелей в каждом уровне и насколько полны данные.
type NightServicesResult struct {
	Rich, Basic, None int
	Pois, WithHours   int // заведений в радиусе отелей (по отелям, с повтором) и с разобранными часами
}

// Run пересчитывает night_services у всех отелей.
func (c *NightServices) Run(ctx context.Context, opts Options) (NightServicesResult, error) {
	var res NightServicesResult
	p, err := NightServicesSpec.Resolve(opts.Params)
	if err != nil {
		return res, err
	}
	targets, err := c.Store.NightTargets(ctx, opts.Filter, p["radius_m"])
	if err != nil {
		return res, fmt.Errorf("заведения: %w", err)
	}
	if len(targets) == 0 && opts.Filter.HotelID != 0 {
		return res, fmt.Errorf("отель %d не найден", opts.Filter.HotelID)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		ns := domain.EvaluateNightServicesWithin(t.Pois, p["radius_m"])
		for _, s := range ns.Services {
			res.Pois += s.Total
		}
		res.WithHours += ns.Parsed
		switch ns.Level {
		case domain.NightRich:
			res.Rich++
		case domain.NightBasic:
			res.Basic++
		default:
			res.None++
		}
		if err := c.Store.SaveHotelEvidence(ctx, t.HotelID, domain.NightServicesKey,
			[]domain.Author{domain.ToolAuthor(domain.NightServicesMethod)}, []domain.Evidence{nightEvidence(t.HotelID, ns, p["radius_m"], now)}); err != nil {
			return res, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
	}
	return res, nil
}

func nightEvidence(hotelID int64, ns domain.NightServices, radiusM float64, now time.Time) domain.Evidence {
	until := now.Add(nightServicesTTL)
	total, unknown := 0, 0
	value := map[string]any{"level": ns.Level}
	var open []map[string]any
	for _, kind := range []string{domain.ServicePharmacy, domain.ServiceShop, domain.ServiceFood} {
		s := ns.Services[kind]
		total += s.Total
		unknown += s.Unknown
		sv := map[string]any{
			"total": s.Total, "unknown_hours": s.Unknown,
			"open_weekday": s.OpenWeekday(), "open_weekend": s.OpenWeekend(),
		}
		if n, ok := s.Nearest(); ok {
			sv["nearest"] = nightPoiValue(n)
		}
		value[kind] = sv
		for _, o := range s.Open {
			e := nightPoiValue(o)
			e["service"] = kind
			open = append(open, e)
		}
	}
	value["pois_total"] = total
	value["hours_known"] = ns.Parsed
	if len(open) > maxNightEvidencePois {
		open = open[:maxNightEvidencePois]
	}
	id := fmt.Sprint(hotelID)
	details := map[string]any{
		"open_at_night":      open,
		"unknown_hours":      unknown,
		"independence_group": domain.NightServicesGroup,
		"params": map[string]any{
			"radius_m": radiusM, "probe_minute": domain.NightProbeMinute,
			"probe_weekday": domain.NightProbeWeekday.String(), "probe_weekend": domain.NightProbeWeekend.String(),
		},
		"assumptions": []string{
			"ночь = 01:00 в среду (ночь будня) и в субботу (ночь выходных), хвост интервала предыдущего вечера считается",
			"заведения без opening_hours или с неразобранными часами считаются «неизвестно», открытыми не считаются: оценка осторожная",
			"праздники, сезонные и месячные правила, восход и закат не поддерживаются: такие часы неизвестны",
			"расстояние по прямой от точки отеля",
		},
	}
	return domain.Evidence{
		EntityType: domain.EntityHotel, EntityID: id, Key: domain.NightServicesKey, Claimed: value,
		Source: domain.SourceOSM, Method: domain.NightServicesMethod, AuthorType: domain.AuthorTool, AuthorID: domain.NightServicesMethod,
		Reliability: domain.NightServicesReliability, ObservedAt: now, ValidUntil: &until, Details: details,
	}
}

func nightPoiValue(o domain.NightOpen) map[string]any {
	m := map[string]any{
		"osm_id": o.Poi.OSMID, "kind": o.Poi.Kind, "dist_m": math.Round(o.Poi.DistM),
		"hours": o.Poi.Hours, "weekday": o.Weekday, "weekend": o.Weekend,
	}
	if o.Poi.Name != "" {
		m["name"] = o.Poi.Name
	}
	return m
}

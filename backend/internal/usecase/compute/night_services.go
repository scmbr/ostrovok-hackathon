package compute

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// maxNightEvidencePois — сколько открытых ночью заведений показывается в evidence.
const maxNightEvidencePois = 15

// NightServicesStore — хранилище для расчёта night_services.
type NightServicesStore interface {
	// NightTargets возвращает отели с заведениями в радиусе radiusM.
	NightTargets(ctx context.Context, flt domain.HotelFilter, radiusM float64) ([]domain.NightTarget, error)
	EvidenceSaver
}

// NightServicesSpec — описание инструмента night_services. Инструмент универсальный: считает любую
// характеристику, стратегия которой в реестре ссылается на него, с набором kinds этой стратегии.
var NightServicesSpec = Spec{
	Name:        "night-services",
	Description: "что открыто ночью рядом с отелем: аптеки, магазины, еда и напитки по opening_hours заведений OSM; параметр kinds сужает виды заведений (ночная аптека — kinds: [pharmacy])",
	Keys:        []string{domain.NightServicesKey},
	AnyKey:      true,
	Scope:       ScopeHotel,
	Params: []Param{
		{"radius_m", "м", "заведения дальше этого расстояния по прямой от отеля не учитываются", domain.NightRadiusM},
	},
	SetParams: []SetParam{
		{"kinds", "виды заведений, которые учитываются (по умолчанию все)", domain.NightServiceKinds},
	},
}

// NightServices считает night_services и производные от него характеристики: что открыто ночью рядом с отелем
// по opening_hours из OSM.
type NightServices struct {
	Registry domain.Registry
	Store    NightServicesStore
	Now      func() time.Time // для тестов; по умолчанию time.Now
}

// NightServicesResult — итоги по каждой характеристике, которую считает инструмент.
type NightServicesResult struct {
	Keys []NightKeyResult
}

// NightKeyResult — сколько отелей в каждом уровне и насколько полны данные по одной характеристике.
type NightKeyResult struct {
	Key               string
	Rich, Basic, None int
	Pois, WithHours   int // заведений в радиусе отелей (по отелям, с повтором) и с разобранными часами
}

// Key возвращает итог по характеристике key.
func (r NightServicesResult) Key(key string) NightKeyResult {
	for _, k := range r.Keys {
		if k.Key == key {
			return k
		}
	}
	return NightKeyResult{Key: key}
}

// Run пересчитывает все характеристики, чьи стратегии в реестре ссылаются на night-services:
// night_services и любые добавленные текстом (например, ночная аптека с kinds: [pharmacy]).
func (c *NightServices) Run(ctx context.Context, opts Options) (NightServicesResult, error) {
	var res NightServicesResult
	uses := c.Registry.ToolUses(NightServicesSpec.Name)
	if len(uses) == 0 {
		return res, fmt.Errorf("в реестре нет характеристик с инструментом %s", NightServicesSpec.Name)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	for _, u := range uses {
		cfg, err := strategyCfg(c.Registry, u.Characteristic.Key, NightServicesSpec.Name, "")
		if err != nil {
			return res, err
		}
		p, err := NightServicesSpec.ResolveLayers(Params(cfg.Strategy.Params), opts.Params)
		if err != nil {
			return res, err
		}
		kinds, err := nightKinds(cfg.Strategy, opts)
		if err != nil {
			return res, err
		}
		kr, err := c.runKey(ctx, opts, cfg, p, kinds, now)
		if err != nil {
			return res, fmt.Errorf("%s: %w", cfg.Key, err)
		}
		res.Keys = append(res.Keys, kr)
	}
	return res, nil
}

// nightKinds берёт виды заведений: из командной строки, иначе из стратегии, иначе все.
func nightKinds(s domain.Strategy, opts Options) ([]string, error) {
	kinds := s.Lists["kinds"]
	if k, ok := opts.Sets["kinds"]; ok {
		kinds = k
	}
	for _, k := range kinds {
		if !slices.Contains(domain.NightServiceKinds, k) {
			return nil, fmt.Errorf("инструмент %s: kinds содержит %q (есть: %s)",
				NightServicesSpec.Name, k, strings.Join(domain.NightServiceKinds, ", "))
		}
	}
	return kinds, nil
}

func (c *NightServices) runKey(ctx context.Context, opts Options, cfg StrategyCfg, p Params, kinds []string, now time.Time) (NightKeyResult, error) {
	res := NightKeyResult{Key: cfg.Key}
	targets, err := c.Store.NightTargets(ctx, opts.Filter, p["radius_m"])
	if err != nil {
		return res, fmt.Errorf("заведения: %w", err)
	}
	if len(targets) == 0 && opts.Filter.HotelID != 0 {
		return res, fmt.Errorf("отель %d не найден", opts.Filter.HotelID)
	}
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		ns := domain.EvaluateNightServicesOf(t.Pois, p["radius_m"], kinds)
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
		ev := nightEvidence(t.HotelID, ns, p["radius_m"], kinds, now, cfg)
		if err := c.Store.SaveHotelEvidence(ctx, t.HotelID, cfg.Key,
			[]domain.Author{domain.ToolAuthor(domain.NightServicesMethod)}, []domain.Evidence{ev}); err != nil {
			return res, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
	}
	return res, nil
}

func nightEvidence(hotelID int64, ns domain.NightServices, radiusM float64, kinds []string, now time.Time, cfg StrategyCfg) domain.Evidence {
	until := cfg.Until(now)
	total, unknown := 0, 0
	// available — есть ли ночью хоть что-то из учтённых видов: значение для bool-характеристик вроде ночной аптеки.
	value := map[string]any{"level": ns.Level, "available": ns.Level != domain.NightNone}
	var open []map[string]any
	shown := kinds
	if len(shown) == 0 {
		shown = domain.NightServiceKinds
	}
	for _, kind := range shown {
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
	details := cfg.WithGroup(map[string]any{
		"open_at_night": open,
		"unknown_hours": unknown,
		"params": map[string]any{
			"radius_m": radiusM, "kinds": shown, "probe_minute": domain.NightProbeMinute,
			"probe_weekday": domain.NightProbeWeekday.String(), "probe_weekend": domain.NightProbeWeekend.String(),
		},
		"assumptions": []string{
			"ночь = 01:00 в среду (ночь будня) и в субботу (ночь выходных), хвост интервала предыдущего вечера считается",
			"заведения без opening_hours или с неразобранными часами считаются «неизвестно», открытыми не считаются: оценка осторожная",
			"праздники, сезонные и месячные правила, восход и закат не поддерживаются: такие часы неизвестны",
			"расстояние по прямой от точки отеля",
		},
	})
	return domain.Evidence{
		EntityType: domain.EntityHotel, EntityID: id, Key: cfg.Key, Claimed: value,
		Source: domain.SourceOSM, Method: domain.NightServicesMethod, AuthorType: domain.AuthorTool, AuthorID: domain.NightServicesMethod,
		Reliability: cfg.Reliability, ObservedAt: now, ValidUntil: &until, Details: details,
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

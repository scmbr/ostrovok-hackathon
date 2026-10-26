package registry

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	embedded "github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

const sample = `
key: night_pharmacy
title: Ночная аптека
description: Есть ли рядом аптека, открытая ночью
level: hotel
value_type: bool
value_field: available
ttl_days: 90
favorable_values: ["true"]
strategies:
  - tool: night-services
    params: { radius_m: 300, kinds: [pharmacy] }
    base_reliability: 0.6
    independence_group: osm_pois
ask_hotelier: Есть ли рядом ночная аптека?
`

func TestParseSplitsNumbersAndLists(t *testing.T) {
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	s := c.Strategies[0]
	if s.Tool != "night-services" || s.BaseReliability != 0.6 || s.IndependenceGroup != "osm_pois" {
		t.Errorf("стратегия: %+v", s)
	}
	if s.Params["radius_m"] != 300 || !reflect.DeepEqual(s.Lists["kinds"], []string{"pharmacy"}) {
		t.Errorf("параметры: %+v %+v", s.Params, s.Lists)
	}
	if !reflect.DeepEqual(c.FavorableValues, []string{"true"}) || c.TTL().Hours() != 90*24 {
		t.Errorf("характеристика: %+v", c)
	}
}

func TestParseRejectsUnknownFieldAndBadParam(t *testing.T) {
	for name, doc := range map[string]string{
		"опечатка в поле":  "key: x\ntitel: y\n",
		"два документа":    "key: x\n---\nkey: y\n",
		"пустой":           "",
		"параметр-словарь": "key: x\nstrategies:\n  - tool: t\n    params: { a: { b: 1 } }\n",
		"число в наборе":   "key: x\nstrategies:\n  - tool: t\n    params: { kinds: [1] }\n",
		"не YAML":          "key: [",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
}

func TestLoadRequiresFileNameEqualsKey(t *testing.T) {
	fsys := fstest.MapFS{"other.yaml": {Data: []byte(sample)}}
	if _, err := Load(fsys); err == nil || !strings.Contains(err.Error(), "не совпадает с именем файла") {
		t.Errorf("ошибка: %v", err)
	}
	fsys = fstest.MapFS{"night_pharmacy.yaml": {Data: []byte(sample)}}
	reg, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("night_pharmacy"); !ok {
		t.Error("характеристика не загружена")
	}
}

// Правила движка правды, собранные из встроенных YAML, совпадают с константами, которые жили в domain.TruthRules
// до переезда в реестр: поведение движка не должно было измениться.
func TestEmbeddedRulesMatchFormerConstants(t *testing.T) {
	reg, err := Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	rules := reg.Rules()
	want := map[string]domain.TruthParams{
		domain.NoiseNightKey: {
			Kind: domain.TruthEnum, ValueField: "level", Order: []string{domain.NoiseQuiet, domain.NoiseModerate, domain.NoiseLoud},
			ConflictSteps: 2, ConflictMinSide: 1, FavorableValues: []string{domain.NoiseQuiet},
			ConflictEntityTypes: []string{domain.EntityFacade},
		},
		domain.WalkToBeachKey: {Kind: domain.TruthNumber, ValueField: "minutes", SignificantDiff: 5, ConflictMinSide: 1},
		domain.WalkToMetroKey: {Kind: domain.TruthNumber, ValueField: "minutes", SignificantDiff: 5, ConflictMinSide: 1},
	}
	for key, w := range want {
		if got := rules[key]; !reflect.DeepEqual(got, w) {
			t.Errorf("%s: правила %+v, были %+v", key, got, w)
		}
	}
}

// Базы надёжности и сроки годности из YAML равны прежним константам инструментов.
func TestEmbeddedReliabilityAndTTLMatchFormerConstants(t *testing.T) {
	reg, err := Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		key, tool, variant string
		reliability        float64
		ttlDays            int
		group              string
	}{
		{domain.NoiseNightKey, "noise-night", "bcn", 0.85, 365, "bcn_noise_2017"},
		{domain.NoiseNightKey, "noise-night", "osm", 0.6, 365, "osm_noise_estimate"},
		{domain.WalkToBeachKey, "walk", "", 0.7, 180, "osm_walk_graph"},
		{domain.WalkToMetroKey, "walk", "", 0.7, 180, "osm_walk_graph"},
		{domain.NightServicesKey, "night-services", "", 0.6, 90, "osm_pois"},
		{domain.ClimateKey, "climate", "", 0.85, 365, "open_meteo_era5"},
		{domain.SeaViewKey, "sea-view", "", 0.85, 365, "buildings_raycast"},
	} {
		s, ok := reg.StrategyFor(tt.key, tt.tool, tt.variant)
		c, _ := reg.Get(tt.key)
		if !ok || s.BaseReliability != tt.reliability || s.IndependenceGroup != tt.group || c.TTLDays != tt.ttlDays {
			t.Errorf("%s/%s/%s: %+v, ttl %d", tt.key, tt.tool, tt.variant, s, c.TTLDays)
		}
	}
}

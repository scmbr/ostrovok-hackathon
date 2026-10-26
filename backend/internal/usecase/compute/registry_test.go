package compute

import (
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Значения тестового реестра повторяют registry/*.yaml в части, которую читают инструменты.
const (
	testBCNReliability   = 0.85
	testOSMReliability   = 0.6
	testWalkReliability  = 0.7
	testNightReliability = 0.6
	testClimateRel       = 0.85
)

func testRegistry(t testing.TB) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry(testCharacteristics())
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// testRules — правила движка правды для рекордера: так же, как БД собирает факты по реестру.
func testRules() map[string]domain.TruthParams {
	reg, _ := domain.NewRegistry(testCharacteristics())
	return reg.Rules()
}

func testCharacteristics() []domain.Characteristic {
	return []domain.Characteristic{
		{Key: domain.SeaViewKey, TTLDays: 365, Strategies: []domain.Strategy{
			{Tool: "sea-view", BaseReliability: 0.85, IndependenceGroup: "buildings_raycast"}}},
		{Key: domain.NoiseNightKey, TTLDays: 365, ValueType: domain.ValueEnum, Values: []string{domain.NoiseQuiet, domain.NoiseModerate, domain.NoiseLoud},
			Ordered: true, ValueField: "level", ConflictSteps: 2, ConflictTheta: 1, ConflictEntities: []string{domain.LevelFacade},
			FavorableValues: []string{domain.NoiseQuiet}, Strategies: []domain.Strategy{
				{Tool: "noise-night", Variant: NoiseVariantBCN, BaseReliability: testBCNReliability, IndependenceGroup: "bcn_noise_2017"},
				{Tool: "noise-night", Variant: NoiseVariantOSM, BaseReliability: testOSMReliability, IndependenceGroup: "osm_noise_estimate"}}},
		{Key: domain.WalkToBeachKey, TTLDays: 180, ValueType: domain.ValueNumber, ValueField: "minutes", SignificantDiff: 5, ConflictTheta: 1, Strategies: []domain.Strategy{
			{Tool: "walk", BaseReliability: testWalkReliability, IndependenceGroup: "osm_walk_graph"}}},
		{Key: domain.WalkToMetroKey, TTLDays: 180, ValueType: domain.ValueNumber, ValueField: "minutes", SignificantDiff: 5, ConflictTheta: 1, Strategies: []domain.Strategy{
			{Tool: "walk", BaseReliability: testWalkReliability, IndependenceGroup: "osm_walk_graph"}}},
		{Key: domain.NightServicesKey, TTLDays: 90, Strategies: []domain.Strategy{
			{Tool: "night-services", BaseReliability: testNightReliability, IndependenceGroup: "osm_pois"}}},
		{Key: "night_pharmacy", TTLDays: 90, Strategies: []domain.Strategy{
			{Tool: "night-services", BaseReliability: testNightReliability, IndependenceGroup: "osm_pois",
				Lists: map[string][]string{"kinds": {domain.ServicePharmacy}}}}},
		{Key: domain.ClimateKey, TTLDays: 365, Strategies: []domain.Strategy{
			{Tool: "climate", BaseReliability: testClimateRel, IndependenceGroup: "open_meteo_era5"}}},
	}
}

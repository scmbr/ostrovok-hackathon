package domain

import (
	"math"
	"testing"
)

func TestEstimateOSMNoiseStreetVsCourtyard(t *testing.T) {
	// Фасад на север (0°): улица secondary в 15 м впереди (0°), двор без дорог, но та же улица сзади (180°).
	street := EstimateOSMNoise(OSMNoiseInput{AzimuthDeg: 0, Roads: []NoiseSourceRef{{Kind: "secondary", DistM: 15, BearingDeg: 0}}})
	yard := EstimateOSMNoise(OSMNoiseInput{AzimuthDeg: 180, Roads: []NoiseSourceRef{{Kind: "secondary", DistM: 15, BearingDeg: 0}}})
	if street.Level != NoiseLoud || street.Dominant != "road:secondary" || street.Exposed != 1 {
		t.Errorf("улица: %+v", street)
	}
	if yard.Level == NoiseLoud || yard.LnDB >= street.LnDB-8 || yard.Exposed != 0 {
		t.Errorf("двор должен быть заметно тише улицы: %+v против %+v", yard, street)
	}
}

func TestEstimateOSMNoiseSourcesAndRadius(t *testing.T) {
	// Без источников — фон, тихо.
	quiet := EstimateOSMNoise(OSMNoiseInput{})
	if quiet.Level != NoiseQuiet || quiet.Dominant != "background" || math.Abs(quiet.LnDB-NoiseBackgroundDB) > 1e-9 {
		t.Errorf("фон: %+v", quiet)
	}
	// Дорога дальше радиуса и неизвестный класс не учитываются.
	far := EstimateOSMNoise(OSMNoiseInput{Roads: []NoiseSourceRef{
		{Kind: "primary", DistM: 200, BearingDeg: 0}, {Kind: "service", DistM: 12, BearingDeg: 0}}})
	if far.Roads != 0 || far.Level != NoiseQuiet {
		t.Errorf("далёкие и неизвестные источники учтены: %+v", far)
	}
	// Клуб рядом с фасадом громче тихой улицы, кафе не считается.
	club := EstimateOSMNoise(OSMNoiseInput{
		Roads:     []NoiseSourceRef{{Kind: "living_street", DistM: 12, BearingDeg: -1}},
		Nightlife: []NoiseSourceRef{{Kind: "nightclub", DistM: 20, BearingDeg: 10}, {Kind: "cafe", DistM: 5, BearingDeg: 0}}})
	if club.Level != NoiseLoud || club.Dominant != "nightlife:nightclub" || club.Nightlife != 1 {
		t.Errorf("клуб: %+v", club)
	}
	// Дальше — тише: тот же бар в 20 и в 100 м.
	near := EstimateOSMNoise(OSMNoiseInput{Nightlife: []NoiseSourceRef{{Kind: "bar", DistM: 20, BearingDeg: 0}}})
	away := EstimateOSMNoise(OSMNoiseInput{Nightlife: []NoiseSourceRef{{Kind: "bar", DistM: 100, BearingDeg: 0}}})
	if near.LnDB <= away.LnDB {
		t.Errorf("бар в 20 м (%.1f) должен быть громче бара в 100 м (%.1f)", near.LnDB, away.LnDB)
	}
}

func TestEstimateOSMNoiseBlockedBySameFacadeBuilding(t *testing.T) {
	// Улица прямо перед фасадом, но между ними чужое здание (боковой фасад квартала): шум ослаблен.
	open := EstimateOSMNoise(OSMNoiseInput{AzimuthDeg: 0, Roads: []NoiseSourceRef{{Kind: "secondary", DistM: 15, BearingDeg: 0}}})
	blocked := EstimateOSMNoise(OSMNoiseInput{AzimuthDeg: 0, Roads: []NoiseSourceRef{{Kind: "secondary", DistM: 15, BearingDeg: 0, Blocked: true}}})
	if blocked.Exposed != 0 || blocked.LnDB >= open.LnDB-10 {
		t.Errorf("закрытый источник: %+v против открытого %+v", blocked, open)
	}
}

func TestExposedWrapsAround(t *testing.T) {
	if !exposed(350, 10) || exposed(350, 180) || !exposed(90, -1) {
		t.Error("угловая разница считается неверно")
	}
}

func TestNoiseConflict(t *testing.T) {
	if NoiseConflict(NoiseQuiet, NoiseQuiet) || !NoiseConflict(NoiseQuiet, NoiseModerate) || !NoiseConflict(NoiseLoud, NoiseQuiet) {
		t.Error("порог конфликта — одна ступень")
	}
}

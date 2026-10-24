package domain

import (
	"math"
	"testing"

	"github.com/paulmach/orb"
)

// fan — 19 лучей к воде в 300 м, у каждого одни и те же препятствия.
func fan(water float64, obstacles ...Obstacle) []SeaRay {
	var rays []SeaRay
	for _, az := range FanAzimuths(135) {
		rays = append(rays, SeaRay{AzimuthDeg: az, HasWater: true, WaterDistM: water, Obstacles: obstacles})
	}
	return rays
}

func TestFanAzimuths(t *testing.T) {
	az := FanAzimuths(10)
	if len(az) != 19 {
		t.Fatalf("лучей %d, want 19", len(az))
	}
	if az[0] != 325 || az[9] != 10 || az[18] != 55 {
		t.Errorf("азимуты %v: ждали 325 … 10 … 55 с переходом через север", az)
	}
}

func TestRayOriginMovesOutward(t *testing.T) {
	mid := orb.Point{2.19, 41.39}
	east := RayOrigin(mid, 90)
	if east[0] <= mid[0] || math.Abs(east[1]-mid[1]) > 1e-9 {
		t.Errorf("на восток: %v", east)
	}
	north := RayOrigin(mid, 0)
	if north[1] <= mid[1] || math.Abs(north[0]-mid[0]) > 1e-9 {
		t.Errorf("на север: %v", north)
	}
}

func TestSeaViewOpen(t *testing.T) {
	v := ComputeSeaView(fan(300), 6)
	if v.MinFloor != 1 {
		t.Errorf("MinFloor = %d, want 1: между фасадом и морем пусто", v.MinFloor)
	}
	if v.NearestWaterM != 300 || v.RaysWithWater != 19 {
		t.Errorf("вода %v м, лучей с водой %d", v.NearestWaterM, v.RaysWithWater)
	}
	if v.Confidence() != 0.85 {
		t.Errorf("confidence %v, want 0.85: помех нет", v.Confidence())
	}
}

// Здание 15 м в 20 м от фасада закрывает нижние этажи: линия взгляда от окна к воде на 450 м
// проходит над ним, только если h·(1 − 20/450) ≥ 15, то есть h ≥ 15,7 м: этаж 6 (16,5 м).
func TestSeaViewBlockedByNeighbour(t *testing.T) {
	cadastre := Obstacle{BuildingID: 7, DistM: 20, HeightM: 15, HeightSource: HeightCatastro}
	v := ComputeSeaView(fan(300, cadastre), 8)
	if v.MinFloor != 6 {
		t.Fatalf("MinFloor = %d, want 6; доли по этажам %v", v.MinFloor, v.Fractions)
	}
	if v.Fractions[4] != 0 || v.Fractions[5] != 1 {
		t.Errorf("доли по этажам %v", v.Fractions)
	}
	if len(v.Blockers) != 1 || v.Blockers[0].BuildingID != 7 {
		t.Errorf("блокирующие здания %+v, want здание 7 на этаже 5", v.Blockers)
	}
	if v.Confidence() != 0.85 {
		t.Errorf("confidence %v, want 0.85: высота из кадастра", v.Confidence())
	}
}

func TestSeaViewConfidenceBySource(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   float64
	}{
		{HeightCatastro, 0.85}, {HeightOSMLevels, 0.6}, {HeightOSMHeight, 0.6}, {HeightDefault, 0.45},
	} {
		v := ComputeSeaView(fan(300, Obstacle{BuildingID: 1, DistM: 20, HeightM: 15, HeightSource: tc.source}), 8)
		if got := v.Confidence(); got != tc.want {
			t.Errorf("%s: confidence %v, want %v", tc.source, got, tc.want)
		}
	}
}

func TestSeaViewBuildingBeyondCoastIgnored(t *testing.T) {
	v := ComputeSeaView(fan(300, Obstacle{BuildingID: 3, DistM: 310, HeightM: 60, HeightSource: HeightCatastro}), 4)
	if v.MinFloor != 1 {
		t.Errorf("MinFloor = %d, want 1: здание за береговой линией не закрывает воду", v.MinFloor)
	}
}

func TestSeaViewNoWater(t *testing.T) {
	var rays []SeaRay
	for _, az := range FanAzimuths(315) {
		rays = append(rays, SeaRay{AzimuthDeg: az})
	}
	v := ComputeSeaView(rays, 5)
	if v.MinFloor != 0 || v.NearestWaterM != -1 || v.RaysWithWater != 0 {
		t.Errorf("%+v: воды в 1,5 км нет, вида быть не должно", v)
	}
}

// Узкий просвет в 3 луча из 19 (16%) вида не даёт, 5 лучей (26%) — даёт.
func TestSeaViewNeedsEnoughRays(t *testing.T) {
	wall := Obstacle{BuildingID: 9, DistM: 15, HeightM: 90, HeightSource: HeightCatastro}
	build := func(open int) []SeaRay {
		rays := fan(300, wall)
		for i := 0; i < open; i++ {
			rays[i].Obstacles = nil
		}
		return rays
	}
	if v := ComputeSeaView(build(3), 5); v.MinFloor != 0 {
		t.Errorf("3 луча из 19: MinFloor = %d, want 0", v.MinFloor)
	}
	if v := ComputeSeaView(build(5), 5); v.MinFloor != 1 {
		t.Errorf("5 лучей из 19: MinFloor = %d, want 1", v.MinFloor)
	}
}

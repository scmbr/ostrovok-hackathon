package domain

import (
	"math"
	"sort"

	"github.com/paulmach/orb"
)

// Параметры расчёта sea_view (метод SeaViewMethod).
const (
	SeaViewKey    = "sea_view"
	SeaViewMethod = "sea_view_raycast@1"

	FloorHeightM  = 3.0   // высота этажа
	WindowHeightM = 1.5   // окно над полом этажа
	SeaOffshoreM  = 150.0 // море «видно», если видна вода в этом расстоянии от береговой линии
	SeaFanHalfDeg = 45.0  // веер лучей по обе стороны от нормали фасада
	SeaFanStepDeg = 5.0
	SeaRayMaxM    = 1500.0 // дальше берега не ищем
	// SeaViewMinFraction — доля лучей веера, с которой этаж считается имеющим вид на море.
	SeaViewMinFraction = 0.25
	// RayOriginOffsetM — начало луча отодвинуто от стены наружу, чтобы не пересекать здание отеля в его же стене.
	RayOriginOffsetM = 0.5

	seaViewBlockersInEvidence = 5
)

// Obstacle — здание на пути луча до берега.
type Obstacle struct {
	BuildingID   int64
	DistM        float64
	HeightM      float64
	HeightSource string
}

// SeaRay — луч от фасада: куда идёт, где пересекает берег с суши в воду и какие здания встретил до берега.
type SeaRay struct {
	AzimuthDeg float64
	HasWater   bool
	WaterDistM float64 // расстояние до береговой линии, если HasWater
	Obstacles  []Obstacle
}

// RayQuery — начало и направление луча для выборки.
type RayQuery struct {
	Origin     orb.Point
	AzimuthDeg float64
}

// StoredFacade — фасад из БД, для которого считается вид.
type StoredFacade struct {
	ID         int64
	Mid        orb.Point
	AzimuthDeg float64
}

// SeaViewTarget — отель, его основное здание и фасады.
type SeaViewTarget struct {
	HotelID    int64
	BuildingID int64
	Levels     int
	Facades    []StoredFacade
}

// FanAzimuths — азимуты лучей веера вокруг нормали фасада.
func FanAzimuths(facadeAzimuth float64) []float64 {
	var out []float64
	for d := -SeaFanHalfDeg; d <= SeaFanHalfDeg+1e-9; d += SeaFanStepDeg {
		out = append(out, math.Mod(facadeAzimuth+d+360, 360))
	}
	return out
}

// RayOrigin — точка в RayOriginOffsetM от середины фасада наружу.
func RayOrigin(mid orb.Point, facadeAzimuth float64) orb.Point {
	pl := newPlane(mid)
	a := facadeAzimuth * math.Pi / 180
	return orb.Point{
		mid[0] + math.Sin(a)*RayOriginOffsetM/pl.kx,
		mid[1] + math.Cos(a)*RayOriginOffsetM/pl.ky,
	}
}

// WindowHeight — высота окна на этаже floor (1 — первый над землёй).
func WindowHeight(floor int) float64 {
	return float64(floor-1)*FloorHeightM + WindowHeightM
}

// Sees сообщает, видна ли с этажа вода в SeaOffshoreM за берегом: ни одно здание не поднимается
// выше линии взгляда от окна до этой точки. Рельеф плоский.
func (r SeaRay) Sees(floor int) bool {
	return len(r.blockers(floor)) == 0 && r.HasWater
}

func (r SeaRay) blockers(floor int) []Obstacle {
	if !r.HasWater {
		return nil
	}
	h := WindowHeight(floor)
	target := r.WaterDistM + SeaOffshoreM
	var out []Obstacle
	for _, o := range r.Obstacles {
		if o.DistM >= r.WaterDistM {
			continue
		}
		if o.HeightM > h*(1-o.DistM/target) {
			out = append(out, o)
		}
	}
	return out
}

// SeaView — вид на море с одного фасада по этажам.
type SeaView struct {
	Levels        int
	MinFloor      int       // 0 — вида нет ни с одного этажа
	Fractions     []float64 // доля лучей с видом по этажам 1..Levels
	NearestWaterM float64   // до ближайшей воды по лучам; -1, если воды нет
	Rays          int
	RaysWithWater int
	// Blockers — здания, закрывающие вид на этаже ниже MinFloor (или на верхнем этаже, если вида нет).
	Blockers []Obstacle
}

// ComputeSeaView считает вид по лучам веера для этажей 1..levels.
func ComputeSeaView(rays []SeaRay, levels int) SeaView {
	if levels < 1 {
		levels = 1
	}
	v := SeaView{Levels: levels, Rays: len(rays), NearestWaterM: -1, Fractions: make([]float64, levels)}
	for _, r := range rays {
		if !r.HasWater {
			continue
		}
		v.RaysWithWater++
		if v.NearestWaterM < 0 || r.WaterDistM < v.NearestWaterM {
			v.NearestWaterM = r.WaterDistM
		}
	}
	for floor := 1; floor <= levels; floor++ {
		seen := 0
		for _, r := range rays {
			if r.Sees(floor) {
				seen++
			}
		}
		if len(rays) > 0 {
			v.Fractions[floor-1] = float64(seen) / float64(len(rays))
		}
		if v.MinFloor == 0 && v.Fractions[floor-1] >= SeaViewMinFraction {
			v.MinFloor = floor
		}
	}

	ref := levels // нет вида — смотрим, что мешает на верхнем этаже
	if v.MinFloor > 0 {
		ref = v.MinFloor - 1
	}
	if ref >= 1 {
		v.Blockers = topBlockers(rays, ref)
	}
	return v
}

// topBlockers — самые высокие здания, закрывающие вид хотя бы на одном луче на этаже floor.
func topBlockers(rays []SeaRay, floor int) []Obstacle {
	byID := map[int64]Obstacle{}
	for _, r := range rays {
		for _, o := range r.blockers(floor) {
			if prev, ok := byID[o.BuildingID]; !ok || o.DistM < prev.DistM {
				byID[o.BuildingID] = o
			}
		}
	}
	out := make([]Obstacle, 0, len(byID))
	for _, o := range byID {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HeightM != out[j].HeightM {
			return out[i].HeightM > out[j].HeightM
		}
		return out[i].BuildingID < out[j].BuildingID
	})
	if len(out) > seaViewBlockersInEvidence {
		out = out[:seaViewBlockersInEvidence]
	}
	return out
}

// Confidence — уверенность по источникам высот зданий, от которых зависит результат:
// кадастр 0,85; высоты OSM 0,6; предположенные 15 м (default) 0,45 (раздел 6 PROJECT_CONTEXT.md).
func (v SeaView) Confidence() float64 {
	conf := 0.85
	for _, b := range v.Blockers {
		switch b.HeightSource {
		case HeightCatastro:
		case HeightOSMHeight, HeightOSMLevels:
			conf = math.Min(conf, 0.6)
		default:
			conf = math.Min(conf, 0.45)
		}
	}
	return conf
}

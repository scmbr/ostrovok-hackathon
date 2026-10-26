package domain

import (
	"math"
	"sort"
)

// Параметры оценки noise_night по OSM (метод NoiseNightOSMMethod): дороги и ночные заведения вокруг фасада.
// Модель эвристическая и не использует карту шума BCN, поэтому независима от неё; значения калибруются на этапе метрик.
const (
	NoiseNightOSMMethod = "noise_night_osm@1"
	SourceOSM           = "osm"
	// NoiseOSMGroup — группа независимости оценки по OSM.
	NoiseOSMGroup = "osm_noise_estimate"
	// NoiseOSMReliability — надёжность расчёта по высотам и тегам OSM (раздел 6 PROJECT_CONTEXT.md).
	NoiseOSMReliability = 0.6

	// NoiseOSMRadiusM — источники дальше этого расстояния от середины фасада не учитываются.
	NoiseOSMRadiusM = 150.0
	// NoiseRefDistM — расстояние, для которого заданы уровни источников.
	NoiseRefDistM = 10.0
	// NoiseBackgroundDB — фоновый ночной уровень города без заметных источников.
	NoiseBackgroundDB = 33.0
	// NoiseShieldDB — ослабление источника, который не виден с фасада (за зданием).
	NoiseShieldDB = 20.0
	// NoiseExposedHalfDeg — источник «виден» фасаду, если направление на него отличается от нормали не больше чем на это.
	NoiseExposedHalfDeg = 75.0
)

// noiseRoadDB — ночной уровень дороги класса highway на расстоянии NoiseRefDistM, дБ.
var noiseRoadDB = map[string]float64{
	"trunk": 70, "primary": 67, "secondary": 64, "tertiary": 60,
	"unclassified": 55, "residential": 55, "living_street": 46, "pedestrian": 38,
}

// noiseNightlifeDB — уровень заведения на расстоянии NoiseRefDistM, дБ. Кафе и рестораны не считаются.
var noiseNightlifeDB = map[string]float64{
	"nightclub": 62, "bar": 52, "pub": 52, "biergarten": 50,
}

// NoiseNightlifeKinds — виды заведений, учитываемых как источники ночного шума.
func NoiseNightlifeKinds() []string {
	kinds := make([]string, 0, len(noiseNightlifeDB))
	for k := range noiseNightlifeDB {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

// NoiseSourceRef — источник шума рядом с фасадом: класс дороги или вид заведения, расстояние и направление на него.
type NoiseSourceRef struct {
	Kind  string // highway (для дорог) или amenity (для заведений)
	DistM float64
	// BearingDeg — азимут от середины фасада к ближайшей точке источника; отрицательный — неизвестен (источник вплотную).
	BearingDeg float64
	// Blocked — на прямой от фасада к источнику стоит чужое здание.
	Blocked bool
}

// OSMNoiseInput — источники вокруг одного фасада.
type OSMNoiseInput struct {
	AzimuthDeg float64 // нормаль фасада наружу
	RadiusM    float64 // источники дальше не учитываются; 0 — NoiseOSMRadiusM
	Roads      []NoiseSourceRef
	Nightlife  []NoiseSourceRef
}

// OSMNoise — оценка шума фасада по OSM.
type OSMNoise struct {
	LnDB      float64
	Level     string
	Dominant  string // самый громкий источник: «road:secondary», «nightlife:bar» или «background»
	Roads     int    // сколько дорог учтено
	Nightlife int
	Exposed   int // источников, видимых фасаду (без ослабления)
}

// EstimateOSMNoise складывает по энергии фоновый уровень и все источники в радиусе. Дорога — линейный источник
// (затухание 10·lg(d/10)), заведение — точечный (20·lg(d/10)); источник вне ±NoiseExposedHalfDeg от нормали фасада
// или закрытый зданием на прямой ослабляется на NoiseShieldDB.
func EstimateOSMNoise(in OSMNoiseInput) OSMNoise {
	type part struct {
		name string
		db   float64
	}
	radius := in.RadiusM
	if radius <= 0 {
		radius = NoiseOSMRadiusM
	}
	parts := []part{{"background", NoiseBackgroundDB}}
	out := OSMNoise{}
	add := func(prefix string, refs []NoiseSourceRef, levels map[string]float64, slope float64, counter *int) {
		for _, r := range refs {
			base, ok := levels[r.Kind]
			if !ok || r.DistM > radius {
				continue
			}
			d := math.Max(r.DistM, NoiseRefDistM)
			db := base - slope*math.Log10(d/NoiseRefDistM)
			if !r.Blocked && exposed(in.AzimuthDeg, r.BearingDeg) {
				out.Exposed++
			} else {
				db -= NoiseShieldDB
			}
			*counter++
			parts = append(parts, part{prefix + ":" + r.Kind, db})
		}
	}
	add("road", in.Roads, noiseRoadDB, 10, &out.Roads)
	add("nightlife", in.Nightlife, noiseNightlifeDB, 20, &out.Nightlife)

	var energy float64
	for _, p := range parts {
		energy += math.Pow(10, p.db/10)
	}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].db > parts[j].db })
	out.LnDB = 10 * math.Log10(energy)
	out.Level = NoiseLevel(out.LnDB)
	out.Dominant = parts[0].name
	return out
}

// exposed: источник в пределах ±NoiseExposedHalfDeg от нормали фасада; неизвестное направление считается видимым.
func exposed(azimuth, bearing float64) bool {
	if bearing < 0 {
		return true
	}
	d := math.Mod(math.Abs(azimuth-bearing), 360)
	if d > 180 {
		d = 360 - d
	}
	return d <= NoiseExposedHalfDeg
}

// NoiseConflictSteps — расхождение источников на столько ступеней и больше инструмент считает в своей сводке.
// Конфликт фасада определяет движок правды (TruthRules): ступени и вес сторон.
const NoiseConflictSteps = 1

// NoiseConflict сообщает, расходятся ли два источника на NoiseConflictSteps ступеней и более (для сводки инструмента).
func NoiseConflict(a, b string) bool {
	d := NoiseLevelRank(a) - NoiseLevelRank(b)
	if d < 0 {
		d = -d
	}
	return d >= NoiseConflictSteps
}

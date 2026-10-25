package domain

import (
	"math"

	"github.com/paulmach/orb"
)

// Параметры noise_night по карте шума BCN (метод NoiseNightBCNMethod).
const (
	NoiseNightKey       = "noise_night"
	NoiseNightBCNMethod = "noise_night_bcn@1"

	// NoiseMapYear — год стратегической карты шума: данные расчётные и устаревшие, это видно в evidence.
	NoiseMapYear = 2017
	// NoiseBCNGroup — независимая группа источников: наборы по фасадам и по улицам из одной модели.
	NoiseBCNGroup = "bcn_noise_2017"
	// NoiseBCNReliability — надёжность расчёта по авторитетной карте (раздел 6 PROJECT_CONTEXT.md).
	NoiseBCNReliability = 0.85

	// Пороги ночного уровня Ln, дБ: до NoiseQuietMaxDB включительно — тихо, до NoiseModerateMaxDB — умеренно.
	NoiseQuietMaxDB    = 45.0
	NoiseModerateMaxDB = 55.0

	// NoiseFacadeBufferM — сегмент карты относится к фасаду отеля, если лежит в этой полосе вокруг него.
	NoiseFacadeBufferM = 2.0
	// NoiseMinOverlapM — минимальная длина сегмента в полосе (или половина сегмента, если он короче).
	NoiseMinOverlapM = 3.0
)

// Уровни noise_night (значения характеристики).
const (
	NoiseQuiet    = "quiet"
	NoiseModerate = "moderate"
	NoiseLoud     = "loud"
)

// NoiseLevel переводит ночной уровень Ln в ступень характеристики.
func NoiseLevel(lnDB float64) string {
	switch {
	case lnDB <= NoiseQuietMaxDB:
		return NoiseQuiet
	case lnDB <= NoiseModerateMaxDB:
		return NoiseModerate
	default:
		return NoiseLoud
	}
}

// NoiseLevelRank — порядок ступеней: чем больше, тем громче.
func NoiseLevelRank(level string) int {
	switch level {
	case NoiseQuiet:
		return 0
	case NoiseModerate:
		return 1
	default:
		return 2
	}
}

// NoiseFacadeSegment — сегмент фасада из карты шума (контур в EPSG:25831).
type NoiseFacadeSegment struct {
	CatastroRef string
	Line        orb.LineString
	LnDB        float64
	LdenDB      float64
	OciLnDB     float64
}

// NoiseSample — сегмент карты, попавший на фасад отеля: уровни и длина внутри полосы фасада.
type NoiseSample struct {
	LnDB     float64
	OciLnDB  float64
	OverlapM float64
}

// FacadeNoise — шум фасада по карте.
type FacadeNoise struct {
	LnDB     float64 // энергетическое среднее по сегментам, взвешенное длиной
	OciLnDB  float64 // максимум шума ночной жизни
	Level    string
	Segments int
}

// ComputeFacadeNoise складывает сегменты карты, лежащие на фасаде: уровни дБ усредняются по энергии
// (10·lg Σ w·10^(L/10) / Σ w), веса — длины внутри полосы фасада. Без сегментов результата нет (false).
func ComputeFacadeNoise(samples []NoiseSample) (FacadeNoise, bool) {
	var sumW, sumE, oci float64
	n := 0
	for _, s := range samples {
		if s.OverlapM <= 0 {
			continue
		}
		n++
		sumW += s.OverlapM
		sumE += s.OverlapM * math.Pow(10, s.LnDB/10)
		oci = math.Max(oci, s.OciLnDB)
	}
	if n == 0 {
		return FacadeNoise{}, false
	}
	ln := 10 * math.Log10(sumE/sumW)
	return FacadeNoise{LnDB: ln, OciLnDB: oci, Level: NoiseLevel(ln), Segments: n}, true
}

// NoiseTarget — отель с фасадами, на которые легли сегменты карты шума.
type NoiseTarget struct {
	HotelID int64
	Facades []NoiseTargetFacade
}

// NoiseTargetFacade — фасад отеля и найденные на нём сегменты карты.
type NoiseTargetFacade struct {
	ID          int64
	AzimuthDeg  float64
	FacesStreet bool
	Courtyard   bool
	Samples     []NoiseSample
}

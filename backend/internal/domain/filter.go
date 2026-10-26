package domain

// HotelFilter ограничивает выборку отелей для инструмента: нулевое поле означает «без ограничения».
type HotelFilter struct {
	RegionID int64 // отели региона
	HotelID  int64 // один отель
}

// NoiseSearch — параметры выборки для noise_night: радиус источников OSM и привязка сегментов карты к фасаду.
type NoiseSearch struct {
	OSMRadiusM    float64
	FacadeBufferM float64
	MinOverlapM   float64
}

// DefaultNoiseSearch — значения по умолчанию.
var DefaultNoiseSearch = NoiseSearch{OSMRadiusM: NoiseOSMRadiusM, FacadeBufferM: NoiseFacadeBufferM, MinOverlapM: NoiseMinOverlapM}

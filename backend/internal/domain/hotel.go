package domain

import "github.com/paulmach/orb"

// Источники высоты здания (buildings.height_source).
const (
	HeightCatastro  = "catastro"
	HeightOSMHeight = "osm_height"
	HeightOSMLevels = "osm_levels"
	HeightDefault   = "default"
)

const (
	// MetersPerLevel — оценка высоты этажа, когда известна только этажность.
	MetersPerLevel = 3.0
	// DefaultHeightM — высота здания, про которое в OSM нет ни height, ни building:levels.
	DefaultHeightM = 15.0
)

// ResolveHeight выбирает высоту здания по тегам OSM: явная height надёжнее этажности.
func ResolveHeight(levels *int, heightM *float64) (outLevels *int, outHeightM float64, source string) {
	switch {
	case heightM != nil && *heightM > 0:
		return levels, *heightM, HeightOSMHeight
	case levels != nil && *levels > 0:
		return levels, float64(*levels) * MetersPerLevel, HeightOSMLevels
	default:
		return nil, DefaultHeightM, HeightDefault
	}
}

// Building — здание из OSM или кадастра. У здания OSM заполнен OSMID, у кадастрового — CatastroRef.
type Building struct {
	OSMID        int64
	CatastroRef  string
	Geom         orb.MultiPolygon
	Levels       *int
	HeightM      float64
	HeightSource string
}

// Hotel — отель из OSM. Для way-отелей BuildingOSMID совпадает с id самого way.
type Hotel struct {
	OSMType string // node | way
	OSMID   int64
	Name    string
	Point   orb.Point // (lon, lat); для way — центр
	// District заполняется по Point.
	District string
}

// StoreID — значение hotels.osm_id. Узлы и пути делят пространство id в OSM,
// поэтому id путей хранятся с минусом. Отели-relation не собираем.
func (h Hotel) StoreID() int64 {
	if h.OSMType == "way" {
		return -h.OSMID
	}
	return h.OSMID
}

// Card — мок базовой карточки Островка (звёзды, цена, завтрак, категории номеров).
type Card struct {
	Mock           bool     `json:"mock"`
	Stars          int      `json:"stars"`
	PriceEUR       int      `json:"price_eur"`
	Breakfast      bool     `json:"breakfast"`
	Wifi           bool     `json:"wifi"`
	RoomCategories []string `json:"room_categories"`
}

// HotelKey — отель в БД: внутренний id и osm_id.
type HotelKey struct {
	ID    int64
	OSMID int64
}

// DemoCandidate — отель, из которых выбирается демо-подмножество.
type DemoCandidate struct {
	ID          int64
	District    string
	HasBuilding bool
	KnownHeight bool // у здания высота не default
	Coastal     bool // не дальше CoastalRadiusM от берега
}

// CoastalRadiusM — расстояние до берега, в пределах которого отель считается береговым.
// На 300 м в OSM набирается 14 отелей, на 400 м — 22 (цель этапа 2 — не меньше 20).
const CoastalRadiusM = 400

// Stats — сводка собранных данных для проверки критерия готовности.
type Stats struct {
	HotelsByDistrict      map[string]int
	HotelsWithBuilding    int
	HotelsWithKnownHeight int
	CoastalHotels         int
	CoastalDemoHotels     int
	HotelsWithCatastro    int // основное здание — кадастровое с этажностью
	HotelsWithOSMBuilding int
	HotelsHeightDiffers   int // у обоих зданий известна высота, и этажность расходится
	BuildingsBySource     map[string]int
	SeaSegments           int
	DemoHotels            int
}

package domain

import (
	"sort"
	"time"
)

// Параметры night_services (метод NightServicesMethod).
const (
	NightServicesKey    = "night_services"
	NightServicesMethod = "night_services_osm@1"
	// NightRadiusM — заведения дальше этого расстояния по прямой от отеля не учитываются (≈4 минуты пешком).
	NightRadiusM = 300.0
	// NightServicesReliability — надёжность по тегам OSM: opening_hours есть не у всех заведений и бывает устаревшим.
	NightServicesReliability = 0.6
	// NightServicesGroup — группа независимости POI OSM.
	NightServicesGroup = "osm_pois"
	// NightProbeMinute — «ночью» значит в 01:00; проверяем ночь будня (среда) и ночь выходных (суббота).
	NightProbeMinute = 60
)

// NightProbeWeekday и NightProbeWeekend — дни, в 01:00 которых проверяется, открыто ли заведение
// (хвост интервала предыдущего вечера считается).
const (
	NightProbeWeekday = time.Wednesday
	NightProbeWeekend = time.Saturday
)

// Уровни night_services.
const (
	NightRich  = "rich"  // есть аптека или магазин ночью и не меньше трёх открытых заведений еды и напитков
	NightBasic = "basic" // есть аптека или магазин ночью, либо хотя бы одно открытое заведение
	NightNone  = "none"  // по данным OSM ночью рядом ничего не открыто
)

// Виды сервисов.
const (
	ServicePharmacy = "pharmacy"
	ServiceShop     = "shop"
	ServiceFood     = "food" // бары, кафе, рестораны, фастфуд
)

// NightPoi — заведение рядом с отелем с тегом opening_hours (или без).
type NightPoi struct {
	OSMID int64
	Kind  string // amenity или shop
	Name  string
	DistM float64
	Hours string // opening_hours; пусто — не указано
}

// NightTarget — отель и заведения в радиусе NightRadiusM.
type NightTarget struct {
	HotelID int64
	Pois    []NightPoi
}

// ServiceKind относит значение amenity/shop к виду сервиса; пустая строка — не учитывается.
func ServiceKind(kind string) string {
	switch kind {
	case "pharmacy":
		return ServicePharmacy
	case "supermarket", "convenience":
		return ServiceShop
	case "bar", "pub", "nightclub", "biergarten", "cafe", "restaurant", "fast_food":
		return ServiceFood
	}
	return ""
}

// NightOpen — заведение, открытое ночью.
type NightOpen struct {
	Poi     NightPoi
	Weekday bool // открыто в 01:00 в ночь будня
	Weekend bool // открыто в 01:00 в ночь выходных
}

// ServiceSummary — итог по виду сервиса.
type ServiceSummary struct {
	Total   int // заведений в радиусе
	Unknown int // без тега opening_hours или с неразобранным
	Open    []NightOpen
}

// NightServices — что открыто ночью рядом с отелем.
type NightServices struct {
	Level    string
	Services map[string]*ServiceSummary
	Parsed   int // заведений с разобранными часами
}

// OpenWeekday — сколько заведений вида открыто в 01:00 в ночь будня.
func (s *ServiceSummary) OpenWeekday() int {
	n := 0
	for _, o := range s.Open {
		if o.Weekday {
			n++
		}
	}
	return n
}

// OpenWeekend — то же для ночи выходных.
func (s *ServiceSummary) OpenWeekend() int {
	n := 0
	for _, o := range s.Open {
		if o.Weekend {
			n++
		}
	}
	return n
}

// Nearest возвращает ближайшее открытое ночью заведение (будни или выходные).
func (s *ServiceSummary) Nearest() (NightOpen, bool) {
	if len(s.Open) == 0 {
		return NightOpen{}, false
	}
	return s.Open[0], true
}

// EvaluateNightServices определяет, что открыто ночью: часы без тега или не разобранные — «неизвестно»
// и открытыми не считаются (оценка осторожная, недооценка вероятнее переоценки). Открытые отсортированы по расстоянию.
func EvaluateNightServices(pois []NightPoi) NightServices {
	return EvaluateNightServicesWithin(pois, NightRadiusM)
}

// EvaluateNightServicesWithin — то же для заведений не дальше radiusM от отеля.
func EvaluateNightServicesWithin(pois []NightPoi, radiusM float64) NightServices {
	res := NightServices{Services: map[string]*ServiceSummary{
		ServicePharmacy: {}, ServiceShop: {}, ServiceFood: {},
	}}
	sorted := append([]NightPoi(nil), pois...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].DistM < sorted[j].DistM })
	for _, p := range sorted {
		kind := ServiceKind(p.Kind)
		if kind == "" || p.DistM > radiusM {
			continue
		}
		sum := res.Services[kind]
		sum.Total++
		if p.Hours == "" {
			sum.Unknown++
			continue
		}
		oh, err := ParseOpeningHours(p.Hours)
		if err != nil {
			sum.Unknown++
			continue
		}
		res.Parsed++
		open := NightOpen{
			Poi:     p,
			Weekday: oh.OpenAt(NightProbeWeekday, NightProbeMinute),
			Weekend: oh.OpenAt(NightProbeWeekend, NightProbeMinute),
		}
		if open.Weekday || open.Weekend {
			sum.Open = append(sum.Open, open)
		}
	}
	essentials := res.Services[ServicePharmacy].OpenWeekday() > 0 || res.Services[ServiceShop].OpenWeekday() > 0
	food := len(res.Services[ServiceFood].Open)
	switch {
	case essentials && food >= 3:
		res.Level = NightRich
	case essentials || food > 0:
		res.Level = NightBasic
	default:
		res.Level = NightNone
	}
	return res
}

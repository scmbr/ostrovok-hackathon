package domain

import (
	"math"
	"time"
)

// Параметры climate_by_month (метод ClimateMethod).
const (
	ClimateKey    = "climate_by_month"
	ClimateMethod = "climate_open_meteo@1"
	// ClimateYears — сколько последних полных лет берётся для месячных норм.
	ClimateYears = 10
	// RainDayMM — день считается дождливым при осадках не меньше этого значения.
	RainDayMM = 1.0
	// SwimmingMinWaterC — вода от этой температуры считается подходящей для купания.
	SwimmingMinWaterC = 20.0

	EntityDistrict  = "district"
	SourceOpenMeteo = "open_meteo"
)

// ClimateDay — суточные значения погоды. Пропуск обозначается NaN.
type ClimateDay struct {
	Date     time.Time
	MeanC    float64
	MaxC     float64
	MinC     float64
	PrecipMM float64
	SunHours float64 // продолжительность солнечного сияния, часы
	SeaC     float64 // средняя за сутки температура поверхности моря
}

// ClimateMonth — норма за календарный месяц по годам периода.
type ClimateMonth struct {
	Month    int      `json:"month"`
	AirMeanC float64  `json:"air_mean_c"`
	AirMaxC  float64  `json:"air_max_c"` // среднее суточных максимумов
	AirMinC  float64  `json:"air_min_c"`
	WaterC   *float64 `json:"water_c"` // nil, если данных о море за месяц нет
	PrecipMM float64  `json:"precip_mm"`
	RainDays float64  `json:"rain_days"`
	SunHours float64  `json:"sun_hours_per_day"`
	Swimming bool     `json:"swimming"` // вода не холоднее SwimmingMinWaterC
}

// ClimateNorms — месячные нормы и сведения о полноте данных.
type ClimateNorms struct {
	Months   [12]ClimateMonth
	Days     int // суток с данными воздуха
	SeaYears int // лет, за которые есть хоть одно значение температуры моря
}

// AggregateClimate сводит суточные значения в нормы по 12 месяцам: температуры — среднее по суткам, осадки — среднее
// месячной суммы по годам, дождливые дни — среднее число дней за месяц по годам, солнце — среднее за сутки,
// температура моря — среднее по имеющимся суткам. NaN пропускаются.
func AggregateClimate(days []ClimateDay) ClimateNorms {
	type acc struct {
		sumMean, sumMax, sumMin, sumSun float64
		n                               int
		sumSea                          float64
		nSea                            int
		precipByYear                    map[int]float64
		rainByYear                      map[int]int
	}
	var months [12]acc
	for i := range months {
		months[i].precipByYear = map[int]float64{}
		months[i].rainByYear = map[int]int{}
	}
	seaYears := map[int]bool{}
	var res ClimateNorms
	for _, d := range days {
		m := &months[d.Date.Month()-1]
		if !math.IsNaN(d.MeanC) {
			res.Days++
			m.n++
			m.sumMean += d.MeanC
			m.sumMax += nz(d.MaxC)
			m.sumMin += nz(d.MinC)
			m.sumSun += nz(d.SunHours)
		}
		if !math.IsNaN(d.PrecipMM) {
			y := d.Date.Year()
			m.precipByYear[y] += d.PrecipMM
			if d.PrecipMM >= RainDayMM {
				m.rainByYear[y]++
			} else if _, ok := m.rainByYear[y]; !ok {
				m.rainByYear[y] = 0
			}
		}
		if !math.IsNaN(d.SeaC) {
			m.sumSea += d.SeaC
			m.nSea++
			seaYears[d.Date.Year()] = true
		}
	}
	for i, m := range months {
		cm := ClimateMonth{Month: i + 1}
		if m.n > 0 {
			n := float64(m.n)
			cm.AirMeanC, cm.AirMaxC, cm.AirMinC, cm.SunHours = m.sumMean/n, m.sumMax/n, m.sumMin/n, m.sumSun/n
		}
		cm.PrecipMM = meanFloat(m.precipByYear)
		if len(m.rainByYear) > 0 {
			var sum int
			for _, c := range m.rainByYear {
				sum += c
			}
			cm.RainDays = float64(sum) / float64(len(m.rainByYear))
		}
		if m.nSea > 0 {
			w := m.sumSea / float64(m.nSea)
			cm.WaterC = &w
			cm.Swimming = w >= SwimmingMinWaterC
		}
		res.Months[i] = cm
	}
	res.SeaYears = len(seaYears)
	return res
}

// SwimmingMonths — номера месяцев, когда вода подходит для купания.
func (n ClimateNorms) SwimmingMonths() []int {
	var out []int
	for _, m := range n.Months {
		if m.Swimming {
			out = append(out, m.Month)
		}
	}
	return out
}

func nz(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	return x
}

func meanFloat(m map[int]float64) float64 {
	if len(m) == 0 {
		return 0
	}
	var sum float64
	for _, v := range m {
		sum += v
	}
	return sum / float64(len(m))
}

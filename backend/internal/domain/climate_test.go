package domain

import (
	"math"
	"testing"
	"time"
)

func day(y int, m time.Month, d int, mean, max, min, precip, sun, sea float64) ClimateDay {
	return ClimateDay{Date: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), MeanC: mean, MaxC: max, MinC: min, PrecipMM: precip, SunHours: sun, SeaC: sea}
}

func TestAggregateClimate(t *testing.T) {
	nan := math.NaN()
	days := []ClimateDay{
		// Июль 2023: две ясных жарких суток, одни дождливые.
		day(2023, time.July, 1, 26, 30, 22, 0, 12, 24),
		day(2023, time.July, 2, 28, 32, 24, 0.5, 14, 26),
		day(2023, time.July, 3, 24, 27, 21, 10, 4, nan),
		// Июль 2024: суток меньше, осадков больше.
		day(2024, time.July, 1, 27, 31, 23, 20, 10, 25),
		// Январь 2024 без данных моря.
		day(2024, time.January, 5, 8, 12, 4, 0, 5, nan),
		// Сутки без данных воздуха не считаются в температурах, но осадки учитываются.
		{Date: time.Date(2024, time.July, 2, 0, 0, 0, 0, time.UTC), MeanC: nan, MaxC: nan, MinC: nan, PrecipMM: 5, SunHours: nan, SeaC: nan},
	}
	n := AggregateClimate(days)
	jul := n.Months[6]
	if math.Abs(jul.AirMeanC-26.25) > 1e-9 || math.Abs(jul.AirMaxC-30) > 1e-9 || math.Abs(jul.AirMinC-22.5) > 1e-9 {
		t.Errorf("июль, воздух: %+v", jul)
	}
	// Осадки: 2023 — 10,5 мм, 2024 — 25 мм → среднее по годам 17,75. Дождливых дней: 2023 — 1, 2024 — 2 → 1,5.
	if math.Abs(jul.PrecipMM-17.75) > 1e-9 || math.Abs(jul.RainDays-1.5) > 1e-9 {
		t.Errorf("июль, осадки: %v мм, %v дней", jul.PrecipMM, jul.RainDays)
	}
	if math.Abs(jul.SunHours-10) > 1e-9 {
		t.Errorf("солнце %v, want 10", jul.SunHours)
	}
	if jul.WaterC == nil || math.Abs(*jul.WaterC-25) > 1e-9 || !jul.Swimming {
		t.Errorf("вода в июле: %+v", jul.WaterC)
	}
	if jan := n.Months[0]; jan.WaterC != nil || jan.Swimming || jan.AirMeanC != 8 {
		t.Errorf("январь: %+v", jan)
	}
	if n.Days != 4+1 || n.SeaYears != 2 {
		t.Errorf("суток %d, лет моря %d", n.Days, n.SeaYears)
	}
	if got := n.SwimmingMonths(); len(got) != 1 || got[0] != 7 {
		t.Errorf("месяцы купания: %v", got)
	}
}

func TestAggregateClimateColdWater(t *testing.T) {
	d := day(2024, time.October, 10, 18, 22, 15, 0, 7, 19.9)
	n := AggregateClimate([]ClimateDay{d})
	if oct := n.Months[9]; oct.WaterC == nil || oct.Swimming {
		t.Errorf("вода 19,9 °C не подходит для купания: %+v", oct)
	}
	d.SeaC = 20
	if !AggregateClimate([]ClimateDay{d}).Months[9].Swimming {
		t.Error("вода ровно 20 °C подходит для купания")
	}
	if got := AggregateClimate(nil); got.Days != 0 || got.Months[0].AirMeanC != 0 {
		t.Errorf("пустой вход: %+v", got)
	}
}

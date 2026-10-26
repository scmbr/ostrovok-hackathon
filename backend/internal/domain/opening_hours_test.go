package domain

import (
	"testing"
	"time"
)

func TestOpeningHoursOpenAt(t *testing.T) {
	const nightMin = 60 // 01:00
	cases := []struct {
		hours string
		day   time.Weekday
		min   int
		want  bool
	}{
		{"24/7", time.Wednesday, nightMin, true},
		{"Mo-Su 00:00-24:00", time.Tuesday, nightMin, true},
		{"Mo-Fr 08:00-22:00; Sa 09:00-14:00", time.Wednesday, 12 * 60, true},
		{"Mo-Fr 08:00-22:00; Sa 09:00-14:00", time.Saturday, 15 * 60, false},
		{"Mo-Fr 08:00-22:00; Sa 09:00-14:00", time.Sunday, 12 * 60, false}, // воскресенье не названо
		// Закрывается за полночь: пятничный интервал 18:00–03:00 открыт в субботу в 01:00, но не в пятницу в 01:00.
		{"Fr 18:00-03:00", time.Saturday, nightMin, true},
		{"Fr 18:00-03:00", time.Friday, nightMin, false},
		{"Fr 18:00-03:00", time.Friday, 23 * 60, true},
		{"Mo-Su 18:00-03:00", time.Wednesday, nightMin, true},
		// Диапазон через воскресенье и несколько интервалов в день.
		{"Fr-Mo 10:00-12:00,17:00-19:00", time.Sunday, 18 * 60, true},
		{"Fr-Mo 10:00-12:00,17:00-19:00", time.Sunday, 14 * 60, false},
		{"Fr-Mo 10:00-12:00,17:00-19:00", time.Wednesday, 11 * 60, false},
		// Позднее правило переопределяет раннее; off закрывает день.
		{"Mo-Su 09:00-22:00; Su off", time.Sunday, 12 * 60, false},
		{"Mo-Su 09:00-22:00; Su off", time.Monday, 12 * 60, true},
		{"09:00-21:00", time.Thursday, 10 * 60, true}, // без дней — все дни
		{"Mo-Su 22:00-02:00", time.Monday, 30, true},  // воскресенье 22:00–02:00 захватывает понедельник 00:30
		// Реальные строки OSM: праздники игнорируются, запятые с пробелами, правила через запятую.
		{"PH,Mo-Su 09:00-22:00", time.Wednesday, 12 * 60, true},
		{"Mo-Fr 09:00-16:00; PH off", time.Wednesday, 10 * 60, true},
		{"Mo, Tu, Th, Fr 12:30-16:00, 19:00-22:30; Sa, Su 19:00-22:30", time.Tuesday, 20 * 60, true},
		{"Mo, Tu, Th, Fr 12:30-16:00, 19:00-22:30; Sa, Su 19:00-22:30", time.Wednesday, 20 * 60, false},
		{"Su-Th 11:00-23:30, Fr,Sa 11:00-02:00", time.Saturday, nightMin, true}, // пятница до 02:00
		{"Su-Th 11:00-23:30, Fr,Sa 11:00-02:00", time.Wednesday, nightMin, false},
		{"Su-Th 11:00-23:30, Fr,Sa 11:00-01:00", time.Saturday, nightMin, false}, // закрылись ровно в 01:00
		{"Tu-Th 18:00-02:00, Fr,Sa 18:00-03:00", time.Wednesday, nightMin, true}, // вторник 18:00–02:00 захватывает среду 01:00
		{"Mo-Th 11:00-00:00; Fr, Sa 11:00-02:00; Su 11:00-00:00", time.Saturday, nightMin, true},
		{"Mo-Su 10:00-22:00; PH open", time.Monday, 11 * 60, true},
	}
	for _, tc := range cases {
		oh, err := ParseOpeningHours(tc.hours)
		if err != nil {
			t.Errorf("%q: %v", tc.hours, err)
			continue
		}
		if got := oh.OpenAt(tc.day, tc.min); got != tc.want {
			t.Errorf("%q, %v %d:%02d: %v, want %v", tc.hours, tc.day, tc.min/60, tc.min%60, got, tc.want)
		}
	}
}

func TestParseOpeningHoursUnsupported(t *testing.T) {
	for _, s := range []string{"", "Mo-Fr sunrise-sunset", "Jan-Mar Mo-Fr 08:00-12:00", "by appointment", "Mo-Fr", "Xx 08:00-12:00", "Mo-Su 06:00-22:00+", "Mo-Fr week 1-4 08:00-12:00"} {
		if _, err := ParseOpeningHours(s); err == nil {
			t.Errorf("%q разобрано без ошибки", s)
		}
	}
}

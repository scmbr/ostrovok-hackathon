package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OpeningHours — разобранный тег opening_hours OSM: интервалы открытия по дням недели в минутах от полуночи дня.
// Конец интервала, перешедшего за полночь, больше 1440. Понимает подмножество синтаксиса: «24/7», дни
// («Mo-Fr», «Mo,We», «Sa»), интервалы через запятую («09:00-14:00,17:00-22:00»), off/closed/open и правила через «;».
// Праздники (PH) игнорируются; месяцы, недели, «+» и восход/закат не поддерживаются: такая строка — ошибка разбора
// (статус неизвестен).
type OpeningHours struct {
	days [7][]minuteSpan // индекс — time.Weekday (0 — воскресенье)
}

type minuteSpan struct{ from, to int }

var weekdayCodes = map[string]time.Weekday{
	"Su": time.Sunday, "Mo": time.Monday, "Tu": time.Tuesday, "We": time.Wednesday,
	"Th": time.Thursday, "Fr": time.Friday, "Sa": time.Saturday,
}

var (
	spanRe = regexp.MustCompile(`^(\d{1,2}:\d{2})-(\d{1,2}:\d{2})(?:[^\d+]|$)`)
	dayRe  = regexp.MustCompile(`^(Mo|Tu|We|Th|Fr|Sa|Su)(?:-(Mo|Tu|We|Th|Fr|Sa|Su))?(?:[^A-Za-z]|$)`)
	wordRe = regexp.MustCompile(`^[A-Za-z]+`)
)

// ParseOpeningHours разбирает строку opening_hours. Более поздние правила переопределяют ранние для тех же дней.
// Правила разделяются «;», а группы дней внутри правила — запятой перед новым днём («Su-Th 11:00-23:30, Fr,Sa 11:00-01:00»).
// Правила только про праздники (PH) пропускаются.
func ParseOpeningHours(s string) (OpeningHours, error) {
	var oh OpeningHours
	s = strings.TrimSpace(s)
	if s == "" {
		return oh, fmt.Errorf("пустая строка")
	}
	var g hoursGroup
	flush := func() error {
		err := g.apply(&oh)
		g = hoursGroup{}
		return err
	}
	for pos := 0; pos < len(s); {
		c := s[pos]
		if c == ' ' || c == ',' {
			pos++
			continue
		}
		if c == ';' {
			if err := flush(); err != nil {
				return oh, err
			}
			pos++
			continue
		}
		rest := s[pos:]
		if m := spanRe.FindStringSubmatchIndex(rest); m != nil {
			from, err1 := parseClock(rest[m[2]:m[3]])
			to, err2 := parseClock(rest[m[4]:m[5]])
			if err1 != nil || err2 != nil {
				return oh, fmt.Errorf("не время: %q", truncateHours(rest))
			}
			if to <= from {
				to += 1440 // закрывается после полуночи
			}
			g.spans = append(g.spans, minuteSpan{from, to})
			g.hasTimes = true
			pos += m[5] // длина интервала без хвостового разделителя
			continue
		}
		if m := dayRe.FindStringSubmatchIndex(rest); m != nil {
			if g.hasTimes || g.off { // новый набор дней после времени
				if err := flush(); err != nil {
					return oh, err
				}
			}
			to := ""
			if m[4] >= 0 {
				to = rest[m[4]:m[5]]
			}
			days, err := expandDays(rest[m[2]:m[3]], to)
			if err != nil {
				return oh, err
			}
			g.days = append(g.days, days...)
			if m[4] >= 0 {
				pos += m[5]
			} else {
				pos += m[3]
			}
			continue
		}
		switch {
		case strings.HasPrefix(rest, "PH"):
			g.holiday = true
			pos += 2
		case strings.HasPrefix(rest, "24/7"):
			g.spans = append(g.spans, minuteSpan{0, 1440})
			g.hasTimes = true
			pos += 4
		default:
			word := wordRe.FindString(rest)
			switch strings.ToLower(word) {
			case "off", "closed":
				g.off = true
			case "open":
				g.hasTimes = true
				g.spans = append(g.spans, minuteSpan{0, 1440})
			default:
				return oh, fmt.Errorf("неподдерживаемый синтаксис: %q", truncateHours(rest))
			}
			pos += len(word)
		}
	}
	if err := flush(); err != nil {
		return oh, err
	}
	return oh, nil
}

// hoursGroup — набор дней с общими интервалами внутри одного правила.
type hoursGroup struct {
	days     []time.Weekday
	spans    []minuteSpan
	hasTimes bool
	off      bool
	holiday  bool
}

// apply записывает группу в расписание. Пустая группа и группа только про праздники пропускаются; без дней —
// все дни; дни без времени и без off — ошибка.
func (g hoursGroup) apply(oh *OpeningHours) error {
	if len(g.days) == 0 && !g.hasTimes && !g.off {
		return nil
	}
	if len(g.days) == 0 && g.holiday {
		return nil // «PH open/off»: праздники не учитываем
	}
	if !g.hasTimes && !g.off {
		return fmt.Errorf("правило без времени")
	}
	days := g.days
	if len(days) == 0 {
		days = []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
	}
	spans := g.spans
	if g.off {
		spans = nil
	}
	for _, d := range days {
		oh.days[d] = spans
	}
	return nil
}

// expandDays раскрывает день или диапазон; диапазон идёт вперёд по неделе, в том числе через воскресенье (Fr-Mo).
func expandDays(from, to string) ([]time.Weekday, error) {
	a, ok := weekdayCodes[from]
	if !ok {
		return nil, fmt.Errorf("неизвестный день %q", from)
	}
	if to == "" {
		return []time.Weekday{a}, nil
	}
	b := weekdayCodes[to]
	var days []time.Weekday
	for d := a; ; d = (d + 1) % 7 {
		days = append(days, d)
		if d == b {
			return days, nil
		}
	}
}

func truncateHours(s string) string {
	if len(s) > 20 {
		return s[:20] + "…"
	}
	return s
}

func parseClock(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("не время: %q", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 48 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("не время: %q", s)
	}
	return hh*60 + mm, nil
}

// OpenAt сообщает, открыто ли заведение в указанный день недели и минуту суток; учитывает интервалы,
// начавшиеся накануне и перешедшие за полночь.
func (oh OpeningHours) OpenAt(day time.Weekday, minute int) bool {
	for _, sp := range oh.days[day] {
		if sp.from <= minute && minute < sp.to {
			return true
		}
	}
	for _, sp := range oh.days[(day+6)%7] {
		if sp.from <= minute+1440 && minute+1440 < sp.to {
			return true
		}
	}
	return false
}

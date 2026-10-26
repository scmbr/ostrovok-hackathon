package app

import "testing"

func TestParseToolFlags(t *testing.T) {
	f, err := parseToolFlags("sea-view", []string{"-hotel", "7", "-param", "fan_half_deg=30", "-param", "ray_max_m=900"})
	if err != nil || f.hotel != 7 || f.params["fan_half_deg"] != 30 || f.params["ray_max_m"] != 900 {
		t.Fatalf("%+v, %v", f, err)
	}
	if f, err := parseToolFlags("climate", []string{"-district", "gothic"}); err != nil || f.district != "gothic" {
		t.Errorf("climate по району: %+v, %v", f, err)
	}
	bad := []struct {
		cmd  string
		args []string
	}{
		{"sea-view", []string{"-district", "gothic"}}, // расчёт по отелям
		{"climate", []string{"-hotel", "1"}},          // расчёт по районам
		{"osm", []string{"-hotel", "1"}},              // не расчётный инструмент
		{"stats", []string{"-param", "a=1"}},
		{"walk", []string{"-param", "speed"}},      // нет значения
		{"walk", []string{"-param", "speed=fast"}}, // не число
		{"walk", []string{"extra"}},
	}
	for _, b := range bad {
		if _, err := parseToolFlags(b.cmd, b.args); err == nil {
			t.Errorf("%s %v: ожидалась ошибка", b.cmd, b.args)
		}
	}
}

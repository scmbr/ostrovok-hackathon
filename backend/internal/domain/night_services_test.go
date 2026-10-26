package domain

import "testing"

func TestEvaluateNightServices(t *testing.T) {
	res := EvaluateNightServices([]NightPoi{
		{OSMID: 1, Kind: "pharmacy", Name: "Farmàcia 24h", DistM: 120, Hours: "24/7"},
		{OSMID: 2, Kind: "pharmacy", Name: "Farmàcia днём", DistM: 60, Hours: "Mo-Fr 09:00-21:00"},
		{OSMID: 3, Kind: "pharmacy", Name: "Без часов", DistM: 30},
		{OSMID: 4, Kind: "bar", Name: "Bar A", DistM: 80, Hours: "Mo-Su 18:00-03:00"},
		{OSMID: 5, Kind: "bar", Name: "Bar B", DistM: 200, Hours: "Fr,Sa 22:00-04:00"}, // открыт только в ночь выходных
		{OSMID: 6, Kind: "nightclub", Name: "Club", DistM: 250, Hours: "Th-Sa 23:00-06:00"},
		{OSMID: 7, Kind: "restaurant", Name: "Нестандартные часы", DistM: 90, Hours: "Mo-Fr sunrise-sunset"},
		{OSMID: 8, Kind: "convenience", Name: "Далеко", DistM: 400, Hours: "24/7"}, // за радиусом
		{OSMID: 9, Kind: "bank", Name: "Не сервис", DistM: 10, Hours: "24/7"},
	})
	ph := res.Services[ServicePharmacy]
	if ph.Total != 3 || ph.Unknown != 1 || ph.OpenWeekday() != 1 || ph.OpenWeekend() != 1 {
		t.Errorf("аптеки: %+v", ph)
	}
	if n, ok := ph.Nearest(); !ok || n.Poi.OSMID != 1 {
		t.Errorf("ближайшая ночная аптека: %+v", n)
	}
	food := res.Services[ServiceFood]
	if food.Total != 4 || food.Unknown != 1 || len(food.Open) != 3 || food.OpenWeekday() != 1 || food.OpenWeekend() != 3 {
		t.Errorf("еда: total %d unknown %d open %d weekday %d weekend %d", food.Total, food.Unknown, len(food.Open), food.OpenWeekday(), food.OpenWeekend())
	}
	// Ночной бар в среду: Bar A. Bar B и клуб ночью выходных (Sa 01:00 = хвост пятницы), в среду закрыты.
	if shop := res.Services[ServiceShop]; shop.Total != 0 {
		t.Errorf("магазин за радиусом учтён: %+v", shop)
	}
	if res.Level != NightRich || res.Parsed != 5 {
		t.Errorf("уровень %s, разобрано %d; want rich (24ч аптека и 3 открытых заведения), 5", res.Level, res.Parsed)
	}
}

func TestEvaluateNightServicesLevels(t *testing.T) {
	cases := []struct {
		name string
		pois []NightPoi
		want string
	}{
		{"пусто", nil, NightNone},
		{"только неизвестные часы", []NightPoi{{Kind: "bar", DistM: 10}, {Kind: "pharmacy", DistM: 20}}, NightNone},
		{"закрыто ночью", []NightPoi{{Kind: "pharmacy", DistM: 10, Hours: "Mo-Su 09:00-21:00"}}, NightNone},
		{"один бар", []NightPoi{{Kind: "bar", DistM: 10, Hours: "Mo-Su 20:00-03:00"}}, NightBasic},
		{"ночная аптека без еды", []NightPoi{{Kind: "pharmacy", DistM: 10, Hours: "24/7"}}, NightBasic},
		{"магазин и три бара", []NightPoi{
			{Kind: "convenience", DistM: 10, Hours: "24/7"},
			{Kind: "bar", DistM: 10, Hours: "24/7"}, {Kind: "pub", DistM: 20, Hours: "24/7"}, {Kind: "cafe", DistM: 30, Hours: "24/7"},
		}, NightRich},
		{"три бара без магазина", []NightPoi{
			{Kind: "bar", DistM: 10, Hours: "24/7"}, {Kind: "pub", DistM: 20, Hours: "24/7"}, {Kind: "cafe", DistM: 30, Hours: "24/7"},
		}, NightBasic},
	}
	for _, tc := range cases {
		if got := EvaluateNightServices(tc.pois).Level; got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

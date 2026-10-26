package compute

import (
	"context"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type nightFake struct {
	recorder
}

func (s *nightFake) NightTargets(context.Context, domain.HotelFilter, float64) ([]domain.NightTarget, error) {
	return []domain.NightTarget{
		{HotelID: 1, Pois: []domain.NightPoi{
			{OSMID: 10, Kind: "pharmacy", Name: "Farmàcia", DistM: 120.4, Hours: "24/7"},
			{OSMID: 11, Kind: "bar", Name: "Bar", DistM: 80, Hours: "Mo-Su 18:00-03:00"},
			{OSMID: 12, Kind: "cafe", DistM: 60}, // часов нет
		}},
		{HotelID: 2}, // рядом ничего нет
	}, nil
}

func TestNightServicesFacts(t *testing.T) {
	store := &nightFake{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	res, err := (&NightServices{Registry: testRegistry(t), Store: store, Now: func() time.Time { return now }}).Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Keys) != 2 {
		t.Fatalf("итогов по характеристикам %d, want 2 (night_services и night_pharmacy)", len(res.Keys))
	}
	res0 := res.Key(domain.NightServicesKey)
	if res0.Basic != 1 || res0.None != 1 || res0.Rich != 0 || res0.Pois != 3 || res0.WithHours != 2 {
		t.Fatalf("итог %+v", res0)
	}

	f := store.saved["1/"+domain.NightServicesKey][0]
	evs := store.ev(f)
	if f.Value["level"] != domain.NightBasic || f.Value["pois_total"] != 3 || f.Value["hours_known"] != 2 {
		t.Errorf("значение: %+v", f.Value)
	}
	pharmacy, _ := f.Value[domain.ServicePharmacy].(map[string]any)
	nearest, _ := pharmacy["nearest"].(map[string]any)
	if pharmacy["open_weekday"] != 1 || nearest["name"] != "Farmàcia" || nearest["dist_m"] != 120.0 {
		t.Errorf("аптека: %+v", pharmacy)
	}
	food, _ := f.Value[domain.ServiceFood].(map[string]any)
	if food["total"] != 2 || food["unknown_hours"] != 1 || food["open_weekday"] != 1 {
		t.Errorf("еда: %+v", food)
	}
	if f.Source != domain.SourceOSM || f.Method != domain.NightServicesMethod || f.Confidence != testNightReliability ||
		f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) {
		t.Errorf("происхождение: %+v", f)
	}
	if len(evs) != 1 || evs[0].Details["unknown_hours"] != 1 ||
		evs[0].Details["independence_group"] != "osm_pois" {
		t.Fatalf("свидетельство: %+v", evs)
	}
	if open, _ := evs[0].Details["open_at_night"].([]map[string]any); len(open) != 2 {
		t.Errorf("открытых ночью в evidence %d, want 2", len(open))
	}

	// Отель без заведений тоже получает факт «ничего»: его отсутствие — результат, а не пробел в данных.
	none := store.saved["2/"+domain.NightServicesKey][0]
	if none.Value["level"] != domain.NightNone || none.Value["pois_total"] != 0 {
		t.Errorf("пустой отель: %+v", none.Value)
	}
}

// Новая характеристика без нового кода: night_pharmacy — тот же инструмент с kinds: [pharmacy]. Учитываются только
// аптеки, значение bool (available), свидетельство пишется под своим ключом.
func TestNightServicesKindsFilter(t *testing.T) {
	store := &nightFake{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	res, err := (&NightServices{Registry: testRegistry(t), Store: store, Now: func() time.Time { return now }}).Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ph := res.Key("night_pharmacy")
	if ph.Basic != 1 || ph.None != 1 || ph.Pois != 1 || ph.Rich != 0 {
		t.Fatalf("итог по аптекам %+v", ph)
	}
	f := store.saved["1/night_pharmacy"][0]
	if f.Key != "night_pharmacy" || f.Value["available"] != true || f.Value["pois_total"] != 1 {
		t.Errorf("факт аптеки: %+v", f)
	}
	if _, has := f.Value[domain.ServiceFood]; has {
		t.Errorf("еда не должна попадать в значение: %+v", f.Value)
	}
	if none := store.saved["2/night_pharmacy"][0]; none.Value["available"] != false {
		t.Errorf("отель без заведений: %+v", none.Value)
	}

	// -param kinds=... из командной строки перекрывает набор из реестра.
	store = &nightFake{}
	if _, err := (&NightServices{Registry: testRegistry(t), Store: store, Now: func() time.Time { return now }}).Run(context.Background(),
		Options{Sets: map[string][]string{"kinds": {domain.ServiceFood}}}); err != nil {
		t.Fatal(err)
	}
	if f := store.saved["1/night_pharmacy"][0]; f.Value["pois_total"] != 2 {
		t.Errorf("kinds=food: %+v", f.Value)
	}
	if _, err := (&NightServices{Registry: testRegistry(t), Store: &nightFake{}}).Run(context.Background(),
		Options{Sets: map[string][]string{"kinds": {"hospital"}}}); err == nil {
		t.Error("неизвестный вид заведения должен быть ошибкой")
	}
}

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

func (s *nightFake) NightTargets(context.Context) ([]domain.NightTarget, error) {
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
	res, err := (&NightServices{Store: store, Now: func() time.Time { return now }}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Basic != 1 || res.None != 1 || res.Rich != 0 || res.Pois != 3 || res.WithHours != 2 {
		t.Fatalf("итог %+v", res)
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
	if f.Source != domain.SourceOSM || f.Method != domain.NightServicesMethod || f.Confidence != domain.NightServicesReliability ||
		f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) {
		t.Errorf("происхождение: %+v", f)
	}
	if len(evs) != 1 || evs[0].Details["unknown_hours"] != 1 ||
		evs[0].Details["independence_group"] != domain.NightServicesGroup {
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

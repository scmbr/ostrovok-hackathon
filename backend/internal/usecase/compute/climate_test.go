package compute

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type climateFake struct {
	from, to time.Time
	points   []orb.Point
	saved    map[string][]domain.Fact
}

func (s *climateFake) Daily(_ context.Context, p orb.Point, from, to time.Time) ([]domain.ClimateDay, error) {
	s.from, s.to = from, to
	s.points = append(s.points, p)
	return []domain.ClimateDay{
		{Date: time.Date(2024, 10, 10, 0, 0, 0, 0, time.UTC), MeanC: 19, MaxC: 23, MinC: 15, PrecipMM: 2, SunHours: 7, SeaC: 21.5},
		{Date: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC), MeanC: 9, MaxC: 13, MinC: 5, PrecipMM: 0, SunHours: 5, SeaC: math.NaN()},
	}, nil
}

func (s *climateFake) SaveDistrictFacts(_ context.Context, district, key string, facts []domain.Fact) error {
	if s.saved == nil {
		s.saved = map[string][]domain.Fact{}
	}
	s.saved[district+"/"+key] = facts
	return nil
}

func TestClimateFacts(t *testing.T) {
	fake := &climateFake{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	c := &Climate{Src: fake, Store: fake, Now: func() time.Time { return now },
		Districts: []domain.District{{Name: "gothic", BBox: domain.BBox{South: 41.379, West: 2.170, North: 41.388, East: 2.182}}}}
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Последние 10 полных лет до 2026: 2016–2025.
	if res.Districts != 1 || res.SeaYears != 1 || fake.from.Year() != 2016 || fake.to != time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("итог %+v, период %v – %v", res, fake.from, fake.to)
	}
	if len(fake.points) != 1 || math.Abs(fake.points[0][0]-2.176) > 1e-9 || math.Abs(fake.points[0][1]-41.3835) > 1e-9 {
		t.Errorf("центр района: %v", fake.points)
	}
	f := fake.saved["gothic/"+domain.ClimateKey][0]
	if f.EntityType != domain.EntityDistrict || f.EntityID != "gothic" || f.Source != domain.SourceOpenMeteo ||
		f.Method != domain.ClimateMethod || f.Confidence != domain.ClimateReliability || f.ValidUntil == nil || !f.ValidUntil.After(now) {
		t.Errorf("происхождение: %+v", f)
	}
	months, _ := f.Value["months"].([]map[string]any)
	if len(months) != 12 {
		t.Fatalf("месяцев %d", len(months))
	}
	oct, jan := months[9], months[0]
	if oct["water_c"] != 21.5 || oct["swimming"] != true || oct["precip_mm"] != 2.0 || oct["air_mean_c"] != 19.0 {
		t.Errorf("октябрь: %+v", oct)
	}
	if jan["water_c"] != nil || jan["swimming"] != false {
		t.Errorf("январь: %+v", jan)
	}
	if sw, _ := f.Value["swimming_months"].([]int); len(sw) != 1 || sw[0] != 10 {
		t.Errorf("месяцы купания: %v", f.Value["swimming_months"])
	}
	if len(f.Evidence) != 1 || f.Evidence[0].Details["independence_group"] != domain.ClimateGroup || f.Evidence[0].Details["sea_years_with_data"] != 1 {
		t.Errorf("свидетельство: %+v", f.Evidence)
	}
}

func TestClimateFailsWithoutData(t *testing.T) {
	empty := &emptyClimate{}
	c := &Climate{Src: empty, Store: empty, Districts: []domain.District{{Name: "x"}}}
	if _, err := c.Run(context.Background()); err == nil {
		t.Error("район без данных не дал ошибку")
	}
}

type emptyClimate struct{}

func (emptyClimate) Daily(context.Context, orb.Point, time.Time, time.Time) ([]domain.ClimateDay, error) {
	return nil, nil
}
func (emptyClimate) SaveDistrictFacts(context.Context, string, string, []domain.Fact) error {
	return nil
}

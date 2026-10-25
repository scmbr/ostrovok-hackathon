package compute

import (
	"context"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type noiseStore struct {
	saved map[int64][]domain.Fact
}

func (s *noiseStore) NoiseTargets(context.Context) ([]domain.NoiseTarget, error) {
	return []domain.NoiseTarget{
		{HotelID: 7, Facades: []domain.NoiseTargetFacade{
			{ID: 70, FacesStreet: true, Samples: []domain.NoiseSample{{LnDB: 62, OciLnDB: 40, OverlapM: 15}}},
			{ID: 71, Courtyard: true, Samples: []domain.NoiseSample{{LnDB: 38, OverlapM: 10}, {LnDB: 40, OverlapM: 8}}},
			{ID: 72}, // сегментов карты нет
		}},
		{HotelID: 8, Facades: []domain.NoiseTargetFacade{{ID: 80}}}, // шума нет ни на одном фасаде
	}, nil
}

func (s *noiseStore) SaveFacts(_ context.Context, hotelID int64, facts []domain.Fact) error {
	if s.saved == nil {
		s.saved = map[int64][]domain.Fact{}
	}
	s.saved[hotelID] = facts
	return nil
}

func TestNoiseNightFacts(t *testing.T) {
	store := &noiseStore{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	res, err := (&NoiseNight{Store: store, Now: func() time.Time { return now }}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Hotels != 1 || res.Facades != 2 {
		t.Fatalf("итог %+v, want 1 отель и 2 фасада: отель без шума и фасад без сегментов пропущены", res)
	}
	if _, ok := store.saved[8]; ok {
		t.Error("отель без шума получил факты")
	}
	facts := store.saved[7]
	if len(facts) != 3 {
		t.Fatalf("фактов %d, want 3: два фасада и отель", len(facts))
	}
	street, courtyard, hotel := facts[0], facts[1], facts[2]
	if street.EntityID != "70" || street.Value["level"] != domain.NoiseLoud {
		t.Errorf("улица: %+v", street.Value)
	}
	if courtyard.EntityID != "71" || courtyard.Value["level"] != domain.NoiseQuiet {
		t.Errorf("двор: %+v", courtyard.Value)
	}
	if hotel.EntityType != domain.EntityHotel || hotel.Value["level"] != domain.NoiseQuiet ||
		hotel.Value["loudest_level"] != domain.NoiseLoud || hotel.Value["quietest_facade"] != int64(71) ||
		hotel.Value["facades_with_data"] != 2 || hotel.Value["facades_total"] != 3 {
		t.Errorf("отель: %+v", hotel.Value)
	}

	for _, f := range facts {
		if f.Source != domain.SourceBCNNoise || f.Method != domain.NoiseNightBCNMethod || f.Key != domain.NoiseNightKey {
			t.Errorf("происхождение факта: %+v", f)
		}
		if f.Confidence != domain.NoiseBCNReliability || f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) {
			t.Errorf("confidence/observed_at/valid_until: %+v", f)
		}
		if len(f.Evidence) != 1 || f.Evidence[0].Reliability != f.Confidence || f.Evidence[0].Details["map"] == nil {
			t.Errorf("свидетельство: %+v", f.Evidence)
		}
	}
}

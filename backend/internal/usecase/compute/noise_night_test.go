package compute

import (
	"math"
	"context"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type noiseStore struct {
	recorder
}

var secondaryAhead = []domain.NoiseSourceRef{{Kind: "secondary", DistM: 15, BearingDeg: 0}}

func (s *noiseStore) NoiseTargets(context.Context, domain.HotelFilter, domain.NoiseSearch) ([]domain.NoiseTarget, error) {
	return []domain.NoiseTarget{
		{HotelID: 7, Facades: []domain.NoiseTargetFacade{
			// Улица: карта и OSM согласны (громко).
			{ID: 70, AzimuthDeg: 0, FacesStreet: true,
				Samples: []domain.NoiseSample{{LnDB: 62, OciLnDB: 40, OverlapM: 15}},
				OSM:     domain.OSMNoiseInput{AzimuthDeg: 0, Roads: secondaryAhead}},
			// Двор: карта и OSM согласны (тихо).
			{ID: 71, AzimuthDeg: 180, Courtyard: true,
				Samples: []domain.NoiseSample{{LnDB: 38, OverlapM: 10}, {LnDB: 40, OverlapM: 8}},
				OSM:     domain.OSMNoiseInput{AzimuthDeg: 180}},
			// Карта не покрывает фасад: только оценка OSM.
			{ID: 72, AzimuthDeg: 0, OSM: domain.OSMNoiseInput{AzimuthDeg: 0, Roads: secondaryAhead}},
			// Карта говорит «тихо», OSM видит магистраль перед фасадом: конфликт.
			{ID: 73, AzimuthDeg: 0,
				Samples: []domain.NoiseSample{{LnDB: 38, OverlapM: 10}},
				OSM:     domain.OSMNoiseInput{AzimuthDeg: 0, Roads: secondaryAhead}},
		}},
		{HotelID: 8, Facades: []domain.NoiseTargetFacade{{ID: 80}}}, // ни карты, ни источников: фоновый уровень
	}, nil
}

func TestNoiseNightFacts(t *testing.T) {
	store := &noiseStore{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	res, err := (&NoiseNight{Store: store, Now: func() time.Time { return now }}).Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hotels != 2 || res.Facades != 5 || res.BCN != 3 || res.OSMOnly != 2 || res.Conflicts != 1 || res.Same != 2 {
		t.Fatalf("итог %+v, want 2 отеля, 5 фасадов, BCN 3, только OSM 2, конфликт 1, согласие 2", res)
	}

	facts := store.saved["7/"+domain.NoiseNightKey]
	if len(facts) != 5 {
		t.Fatalf("фактов %d, want 5: четыре фасада и отель", len(facts))
	}
	street, yard, osmOnly, disputed, hotel := facts[0], facts[1], facts[2], facts[3], facts[4]

	if street.Value["level"] != domain.NoiseLoud || street.Source != domain.SourceBCNNoise || street.Conflict != nil || len(street.EvidenceIDs) != 2 {
		t.Errorf("улица: %+v, свидетельств %d", street.Value, len(street.EvidenceIDs))
	}
	if yard.Value["level"] != domain.NoiseQuiet || yard.Conflict != nil {
		t.Errorf("двор: %+v", yard.Value)
	}
	if osmOnly.Source != domain.SourceOSM || osmOnly.Confidence != domain.NoiseOSMReliability ||
		len(osmOnly.EvidenceIDs) != 1 || osmOnly.Value["dominant"] == nil {
		t.Errorf("только OSM: %+v, свидетельств %d", osmOnly.Value, len(osmOnly.EvidenceIDs))
	}
	// Расхождение на две ступени с весом OSM-оценки ниже θ: значение по карте BCN, уверенность снижена
	// (σ(1.7346 − 0.4055) = 0.7908), это weak_disagreement, а не конфликт.
	if disputed.Conflict != nil || !disputed.WeakDisagreement || disputed.Value["level"] != domain.NoiseQuiet ||
		len(disputed.EvidenceIDs) != 2 || math.Abs(disputed.Confidence-0.7908) > 1e-3 {
		t.Errorf("расхождение: %+v, конфликт %v, weak %v, уверенность %.4f", disputed.Value, disputed.Conflict, disputed.WeakDisagreement, disputed.Confidence)
	}


	if hotel.EntityType != domain.EntityHotel || hotel.Value["level"] != domain.NoiseQuiet ||
		hotel.Value["loudest_level"] != domain.NoiseLoud || hotel.Value["quietest_facade"] != int64(73) ||
		hotel.Value["facades_total"] != 4 {
		t.Errorf("отель: %+v", hotel.Value)
	}
	for _, e := range store.ev(hotel) {
		if e.EntityType != domain.EntityHotel || e.EntityID != "7" || e.Details["quietest_facade"] != int64(73) {
			t.Errorf("свидетельство отеля: %+v", e)
		}
	}

	bare := store.saved["8/"+domain.NoiseNightKey]
	if len(bare) != 2 || bare[1].Value["level"] != domain.NoiseQuiet || bare[1].Source != domain.SourceOSM {
		t.Errorf("отель без источников: %+v", bare)
	}

	for _, fs := range store.saved {
		for _, f := range fs {
			if f.Key != domain.NoiseNightKey || f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) || len(f.EvidenceIDs) == 0 {
				t.Errorf("общие поля факта: %+v", f)
			}
			for _, e := range store.ev(f) {
				if e.EntityType != f.EntityType || e.EntityID != f.EntityID || e.Reliability <= 0 || e.Details["independence_group"] == nil && e.Details["derived_from"] == nil {
					t.Errorf("свидетельство: %+v", e)
				}
			}
		}
	}
}

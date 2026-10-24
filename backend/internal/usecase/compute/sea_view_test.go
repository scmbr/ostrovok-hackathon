package compute

import (
	"context"
	"testing"
	"time"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// seaStore: фасад на восток (вода в 300 м, дом 12 м в 20 м закрывает нижние этажи)
// и фасад на запад (воды нет).
type seaStore struct {
	saved []domain.Fact
}

func (s *seaStore) SeaViewTargets(context.Context) ([]domain.SeaViewTarget, error) {
	return []domain.SeaViewTarget{{
		HotelID: 5, BuildingID: 1, Levels: 6,
		Facades: []domain.StoredFacade{
			{ID: 10, Mid: orb.Point{2.19, 41.39}, AzimuthDeg: 90},
			{ID: 11, Mid: orb.Point{2.19, 41.39}, AzimuthDeg: 270},
		},
	}}, nil
}

func (s *seaStore) SeaRays(_ context.Context, q []domain.RayQuery) ([]domain.SeaRay, error) {
	out := make([]domain.SeaRay, len(q))
	for i, r := range q {
		if r.AzimuthDeg > 0 && r.AzimuthDeg < 180 {
			out[i] = domain.SeaRay{AzimuthDeg: r.AzimuthDeg, HasWater: true, WaterDistM: 300, Obstacles: []domain.Obstacle{
				{BuildingID: 77, DistM: 20, HeightM: 12, HeightSource: domain.HeightOSMLevels},
			}}
		} else {
			out[i] = domain.SeaRay{AzimuthDeg: r.AzimuthDeg}
		}
	}
	return out, nil
}

func (s *seaStore) SaveFacts(_ context.Context, _ int64, facts []domain.Fact) error {
	s.saved = facts
	return nil
}

func TestSeaViewFacts(t *testing.T) {
	store := &seaStore{}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	n, err := (&SeaView{Store: store, Now: func() time.Time { return now }}).Run(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Run: %d, %v", n, err)
	}
	if len(store.saved) != 3 {
		t.Fatalf("фактов %d, want 3: два фасада и отель", len(store.saved))
	}
	east, west, hotel := store.saved[0], store.saved[1], store.saved[2]

	// Окно должно подняться над 12 м: h·(1 − 20/450) ≥ 12 → h ≥ 12,56 м → этаж 5 (13,5 м).
	if east.Value["min_floor"] != 5 {
		t.Errorf("восточный фасад: min_floor = %v, want 5", east.Value["min_floor"])
	}
	if west.Value["min_floor"] != nil {
		t.Errorf("западный фасад: min_floor = %v, want nil: воды нет", west.Value["min_floor"])
	}
	if hotel.Value["min_floor"] != 5 || hotel.Value["facade_id"] != int64(10) {
		t.Errorf("отель: %v", hotel.Value)
	}

	for _, f := range store.saved {
		if f.Source != domain.SourceComputed || f.Method != domain.SeaViewMethod || f.Key != domain.SeaViewKey {
			t.Errorf("происхождение факта не заполнено: %+v", f)
		}
		if f.Confidence <= 0 || f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) {
			t.Errorf("confidence/observed_at/valid_until: %+v", f)
		}
		if len(f.Evidence) == 0 {
			t.Errorf("пустой evidence: %+v", f)
		}
	}
	// Вид зависит от высоты OSM (levels), поэтому уверенность ниже кадастровой.
	if east.Confidence != 0.6 {
		t.Errorf("confidence = %v, want 0.6 (препятствие по высоте OSM)", east.Confidence)
	}
}

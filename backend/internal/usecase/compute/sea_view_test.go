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
	recorder
}

func (s *seaStore) SeaViewTargets(context.Context, domain.HotelFilter) ([]domain.SeaViewTarget, error) {
	return []domain.SeaViewTarget{{
		HotelID: 5, BuildingID: 1,
		Facades: []domain.StoredFacade{
			{ID: 10, Mid: orb.Point{2.19, 41.39}, AzimuthDeg: 90, Levels: 6},
			{ID: 11, Mid: orb.Point{2.19, 41.39}, AzimuthDeg: 270, Levels: 3},
		},
	}}, nil
}

func (s *seaStore) SeaRays(_ context.Context, q []domain.RayQuery, _ float64) ([]domain.SeaRay, error) {
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

func TestSeaViewFacts(t *testing.T) {
	store := &seaStore{}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	n, err := (&SeaView{Registry: testRegistry(t), Store: store, Now: func() time.Time { return now }}).Run(context.Background(), Options{})
	if err != nil || n != 1 {
		t.Fatalf("Run: %d, %v", n, err)
	}
	saved := store.saved["5/"+domain.SeaViewKey]
	if len(saved) != 3 {
		t.Fatalf("фактов %d, want 3: два фасада и отель", len(saved))
	}
	east, west, hotel := saved[0], saved[1], saved[2]

	// Окно должно подняться над 12 м: h·(1 − 20/450) ≥ 12 → h ≥ 12,56 м → этаж 5 (13,5 м).
	if east.Value["min_floor"] != 5 {
		t.Errorf("восточный фасад: min_floor = %v, want 5", east.Value["min_floor"])
	}
	if west.Value["min_floor"] != nil {
		t.Errorf("западный фасад: min_floor = %v, want nil: воды нет", west.Value["min_floor"])
	}
	if west.Value["levels"] != 3 || east.Value["levels"] != 6 {
		t.Errorf("этажность на фасад: запад %v, восток %v, want 3 и 6", west.Value["levels"], east.Value["levels"])
	}
	if hotel.Value["min_floor"] != 5 || hotel.Value["facade_id"] != int64(10) {
		t.Errorf("отель: %v", hotel.Value)
	}

	for _, f := range saved {
		if f.Source != domain.SourceComputed || f.Method != domain.SeaViewMethod || f.Key != domain.SeaViewKey {
			t.Errorf("происхождение факта не заполнено: %+v", f)
		}
		if f.Confidence <= 0 || f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) {
			t.Errorf("confidence/observed_at/valid_until: %+v", f)
		}
		if len(f.EvidenceIDs) != 1 {
			t.Fatalf("свидетельств %d, want 1: %+v", len(f.EvidenceIDs), f)
		}
		e := store.ev(f)[0]
		if e.EntityType != f.EntityType || e.EntityID != f.EntityID || e.Key != f.Key ||
			e.Source != f.Source || e.Method != f.Method || e.AuthorType != domain.AuthorTool {
			t.Errorf("свидетельство не согласовано с фактом: %+v", e)
		}
		if e.Reliability != f.Confidence || len(e.Details) == 0 || e.Claimed == nil {
			t.Errorf("надёжность/детали/значение свидетельства: %+v", e)
		}
	}
	// Вид зависит от высоты OSM (levels), поэтому уверенность ниже кадастровой.
	if east.Confidence != 0.6 {
		t.Errorf("confidence = %v, want 0.6 (препятствие по высоте OSM)", east.Confidence)
	}
}

// Башня на подиуме: вид с 1-го этажа подиума (2 этажа) для отеля не показатель, лучший фасад — башенный.
type towerStore struct{ seaStore }

func (s *towerStore) SeaViewTargets(context.Context, domain.HotelFilter) ([]domain.SeaViewTarget, error) {
	return []domain.SeaViewTarget{{
		HotelID: 6, BuildingID: 2,
		Facades: []domain.StoredFacade{
			{ID: 20, Mid: orb.Point{2.19, 41.39}, AzimuthDeg: 90, Levels: 2},   // подиум
			{ID: 21, Mid: orb.Point{2.19, 41.39}, AzimuthDeg: 100, Levels: 20}, // башня
		},
	}}, nil
}

func (s *towerStore) SeaRays(_ context.Context, q []domain.RayQuery, _ float64) ([]domain.SeaRay, error) {
	out := make([]domain.SeaRay, len(q))
	for i, r := range q {
		out[i] = domain.SeaRay{AzimuthDeg: r.AzimuthDeg, HasWater: true, WaterDistM: 300}
		if r.AzimuthDeg > 110 { // луч башни закрыт на нижних этажах, веер подиума открыт
			out[i].Obstacles = []domain.Obstacle{{BuildingID: 1, DistM: 20, HeightM: 12, HeightSource: domain.HeightCatastro}}
		}
	}
	return out, nil
}

func TestSeaViewHotelFactSkipsLowPodium(t *testing.T) {
	store := &towerStore{}
	if _, err := (&SeaView{Registry: testRegistry(t), Store: store}).Run(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	saved := store.saved["6/"+domain.SeaViewKey]
	hotel := saved[len(saved)-1]
	if hotel.Value["facade_id"] != int64(21) {
		t.Errorf("лучший фасад %v, want 21 (башня): подиум в 2 этажа не показатель", hotel.Value["facade_id"])
	}
}

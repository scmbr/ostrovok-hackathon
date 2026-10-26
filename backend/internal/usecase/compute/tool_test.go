package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func TestSpecResolve(t *testing.T) {
	got, err := SeaViewSpec.Resolve(Params{"fan_half_deg": 30})
	if err != nil {
		t.Fatal(err)
	}
	if got["fan_half_deg"] != 30 || got["fan_step_deg"] != domain.SeaFanStepDeg || got["ray_max_m"] != domain.SeaRayMaxM {
		t.Errorf("значения: заданное и умолчания: %v", got)
	}
	if _, err := SeaViewSpec.Resolve(Params{"fan_half": 30, "nope": 1}); err == nil ||
		!strings.Contains(err.Error(), "fan_half, nope") || !strings.Contains(err.Error(), "fan_half_deg") {
		t.Errorf("неизвестные параметры должны давать ошибку со списком допустимых: %v", err)
	}
}

func TestSpecsAreUnique(t *testing.T) {
	names, keys := map[string]bool{}, map[string]bool{}
	for _, s := range Specs {
		if s.Name == "" || s.Description == "" || len(s.Keys) == 0 || len(s.Params) == 0 {
			t.Errorf("описание инструмента неполное: %+v", s)
		}
		if names[s.Name] {
			t.Errorf("повтор имени %s", s.Name)
		}
		names[s.Name] = true
		for _, k := range s.Keys {
			if keys[k] {
				t.Errorf("характеристика %s у двух инструментов", k)
			}
			keys[k] = true
		}
		seen := map[string]bool{}
		for _, p := range s.Params {
			if p.Name == "" || p.Description == "" || seen[p.Name] {
				t.Errorf("%s: параметр %+v", s.Name, p)
			}
			seen[p.Name] = true
		}
	}
}

// Параметры доходят до расчёта, а фильтр отеля — до хранилища.
func TestSeaViewParamsAndFilter(t *testing.T) {
	store := &filterSea{}
	opts := Options{Filter: domain.HotelFilter{HotelID: 5}, Params: Params{"fan_half_deg": 10, "fan_step_deg": 5, "ray_max_m": 900}}
	if _, err := (&SeaView{Registry: testRegistry(t), Store: store}).Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if store.filter.HotelID != 5 || store.rayMaxM != 900 || store.rays != 5*2 {
		t.Errorf("фильтр %+v, ray_max_m %v, лучей %d, want отель 5, 900, 10 (веер ±10° с шагом 5° = 5 лучей на фасад)", store.filter, store.rayMaxM, store.rays)
	}
	if _, err := (&SeaView{Registry: testRegistry(t), Store: store}).Run(context.Background(), Options{Params: Params{"bogus": 1}}); err == nil {
		t.Error("неизвестный параметр должен давать ошибку")
	}
	if _, err := (&SeaView{Registry: testRegistry(t), Store: &emptySea{}}).Run(context.Background(), Options{Filter: domain.HotelFilter{HotelID: 404}}); err == nil {
		t.Error("отель без фасадов или несуществующий должен давать ошибку")
	}
}

type filterSea struct {
	seaStore
	filter  domain.HotelFilter
	rayMaxM float64
	rays    int
}

func (s *filterSea) SeaViewTargets(ctx context.Context, f domain.HotelFilter) ([]domain.SeaViewTarget, error) {
	s.filter = f
	return s.seaStore.SeaViewTargets(ctx, f)
}

func (s *filterSea) SeaRays(ctx context.Context, q []domain.RayQuery, rayMaxM float64) ([]domain.SeaRay, error) {
	s.rayMaxM, s.rays = rayMaxM, len(q)
	return s.seaStore.SeaRays(ctx, q, rayMaxM)
}

type emptySea struct{ seaStore }

func (emptySea) SeaViewTargets(context.Context, domain.HotelFilter) ([]domain.SeaViewTarget, error) {
	return nil, nil
}

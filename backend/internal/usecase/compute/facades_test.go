package compute

import (
	"context"
	"math"
	"sort"
	"testing"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// fakeStore имитирует соседей: здания вплотную слева и справа от дома 20 × 10 м,
// дорога — только к югу от дома.
type fakeStore struct {
	building domain.HotelBuilding
	saved    []domain.Facade
	stats    domain.ContourStats
}

func (f *fakeStore) HotelBuildings(context.Context) ([]domain.HotelBuilding, error) {
	return []domain.HotelBuilding{f.building}, nil
}

func (f *fakeStore) BlindFractions(_ context.Context, _ int64, edges []domain.Edge) ([]float64, error) {
	minX, maxX := math.Inf(1), math.Inf(-1)
	for _, e := range edges {
		minX, maxX = math.Min(minX, e.A[0]), math.Max(maxX, e.A[0])
	}
	out := make([]float64, len(edges))
	for i, e := range edges {
		onWest := e.A[0] == minX && e.B[0] == minX
		onEast := e.A[0] == maxX && e.B[0] == maxX
		if onWest || onEast {
			out[i] = 1
		}
	}
	return out, nil
}

func (f *fakeStore) FacesStreet(_ context.Context, _ int64, probes []domain.Probe) ([]bool, error) {
	out := make([]bool, len(probes))
	for i, p := range probes {
		out[i] = math.Round(p.AzimuthDeg) == 180
	}
	return out, nil
}

func (f *fakeStore) ReplaceFacades(_ context.Context, _ int64, facades []domain.Facade, st domain.ContourStats) error {
	f.saved, f.stats = facades, st
	return nil
}

func TestFacadesBuildingBetweenNeighbours(t *testing.T) {
	const kx, ky = 83_300.0, 111_195.0 // метров на градус около 41,39° с.ш.
	p := func(x, y float64) orb.Point { return orb.Point{2.19 + x/kx, 41.39 + y/ky} }
	ring := orb.Ring{p(0, 0), p(20, 0), p(20, 10), p(0, 10), p(0, 0)}
	store := &fakeStore{building: domain.HotelBuilding{
		HotelID: 7, BuildingID: 1, Geom: orb.MultiPolygon{{ring}},
	}}

	n, err := (&Facades{Store: store}).Run(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Run: %d, %v", n, err)
	}
	if store.stats.Segments != 4 || store.stats.Blind != 2 {
		t.Errorf("stats %+v, want 4 сегмента, из них 2 глухих", store.stats)
	}
	if len(store.saved) != 2 {
		t.Fatalf("фасадов %d, want 2: торцы у соседей — глухие стены", len(store.saved))
	}
	sort.Slice(store.saved, func(i, j int) bool { return store.saved[i].AzimuthDeg < store.saved[j].AzimuthDeg })
	north, south := store.saved[0], store.saved[1]
	if math.Round(north.AzimuthDeg) != 0 || north.FacesStreet {
		t.Errorf("северный фасад: %+v", north)
	}
	if math.Round(south.AzimuthDeg) != 180 || !south.FacesStreet {
		t.Errorf("южный фасад: %+v", south)
	}
	if north.HotelID != 7 || south.HotelID != 7 {
		t.Errorf("hotel_id не проставлен")
	}
}

package compute

import (
	"context"
	"testing"
	"time"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// walkFake: линия узлов 1 —100 м— 2 —100 м— 3 —100 м— 4. Отель A у узла 1, отель B у узла 4,
// отель C далеко от графа. Метро привязано к ребру 2–3 (90 м до узла 2, 30 м до узла 3),
// пляж — к ребру 3–4 (150 м до узла 3, 50 м до узла 4).
type walkFake struct {
	recorder
}

func (s *walkFake) LoadWalkEdges(context.Context) ([]domain.WalkEdge, error) {
	return []domain.WalkEdge{
		{ID: 1, Source: 1, Target: 2, LengthM: 100},
		{ID: 2, Source: 2, Target: 3, LengthM: 100},
		{ID: 3, Source: 3, Target: 4, LengthM: 100},
	}, nil
}

func (s *walkFake) HotelPoints(context.Context) ([]domain.HotelPoint, error) {
	return []domain.HotelPoint{
		{ID: 1, Name: "A", Point: orb.Point{2.17, 41.38}},
		{ID: 2, Name: "B", Point: orb.Point{2.18, 41.38}},
		{ID: 3, Name: "C", Point: orb.Point{9, 9}},
	}, nil
}

func (s *walkFake) Destinations(_ context.Context, kind string) ([]domain.Destination, error) {
	if kind == domain.DestMetro {
		return []domain.Destination{{OSMID: 500, Kind: kind, Name: "Jaume I", Point: orb.Point{2.175, 41.38}}}, nil
	}
	return []domain.Destination{{OSMID: 600, Kind: kind, Name: "Platja", Point: orb.Point{2.181, 41.38}}}, nil
}

func (s *walkFake) SnapPoints(_ context.Context, pts []orb.Point, _ float64) ([]domain.WalkSnap, []bool, error) {
	snaps := make([]domain.WalkSnap, len(pts))
	ok := make([]bool, len(pts))
	for i, p := range pts {
		switch p {
		case orb.Point{2.17, 41.38}: // отель A: у узла 1
			snaps[i], ok[i] = domain.WalkSnap{EdgeID: 1, Source: 1, Target: 2, ToSourceM: 10, ToTargetM: 120}, true
		case orb.Point{2.18, 41.38}: // отель B: у узла 4
			snaps[i], ok[i] = domain.WalkSnap{EdgeID: 3, Source: 3, Target: 4, ToSourceM: 110, ToTargetM: 10}, true
		case orb.Point{2.175, 41.38}: // метро у узла 3
			snaps[i], ok[i] = domain.WalkSnap{EdgeID: 2, Source: 2, Target: 3, ToSourceM: 90, ToTargetM: 30}, true
		case orb.Point{2.181, 41.38}: // пляж за узлом 4
			snaps[i], ok[i] = domain.WalkSnap{EdgeID: 3, Source: 3, Target: 4, ToSourceM: 150, ToTargetM: 50}, true
		}
	}
	return snaps, ok, nil
}

func (s *walkFake) EdgeGeoms(_ context.Context, ids []int64) (map[int64]orb.LineString, error) {
	all := map[int64]orb.LineString{
		1: {{2.170, 41.38}, {2.171, 41.38}},
		2: {{2.171, 41.38}, {2.172, 41.38}},
		3: {{2.172, 41.38}, {2.173, 41.38}},
	}
	out := map[int64]orb.LineString{}
	for _, id := range ids {
		out[id] = all[id]
	}
	return out, nil
}

func TestWalkFacts(t *testing.T) {
	store := &walkFake{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	res, err := (&Walk{Store: store, Now: func() time.Time { return now }}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Hotels != 2 || res.Unsnapped != 1 || res.Beach != 2 || res.Metro != 2 || res.BeachTargets != 1 || res.MetroTargets != 1 {
		t.Fatalf("итог %+v", res)
	}

	// Отель A: старт у узла 1 за 10 м, путь 1→2 = 100 м, метро за 90 м от узла 2: 10 + 100 + 90 = 200 м → 3 минуты.
	metroA := store.saved["1/"+domain.WalkToMetroKey]
	if len(metroA) != 1 {
		t.Fatalf("фактов метро у A: %d", len(metroA))
	}
	f := metroA[0]
	if f.Value["minutes"] != 3 || f.Value["distance_m"] != 200.0 || f.Value["target"] != "Jaume I" || f.Value["target_kind"] != domain.DestMetro {
		t.Errorf("метро A: %+v", f.Value)
	}
	if f.Source != domain.SourceOSM || f.Method != domain.WalkMethod || f.Confidence != domain.WalkReliability ||
		f.ObservedAt != now || f.ValidUntil == nil || !f.ValidUntil.After(now) {
		t.Errorf("происхождение: %+v", f)
	}
	evs := store.ev(f)
	if len(evs) != 1 || evs[0].Details["independence_group"] != domain.WalkGroup || evs[0].Reliability != f.Confidence {
		t.Fatalf("свидетельство: %+v", evs)
	}
	// Путь: одно ребро 1–2.
	path, _ := evs[0].Details["path"].([][2]float64)
	if len(path) != 2 || path[0] != [2]float64{2.17, 41.38} || path[1] != [2]float64{2.171, 41.38} {
		t.Errorf("путь: %v", path)
	}

	// Отель B: пляж за 10 + 50 м; метро: 10 до узла 4, 100 до узла 3, ещё 30 до метро — 140 м.
	if b := store.saved["2/"+domain.WalkToBeachKey][0]; b.Value["distance_m"] != 60.0 || b.Value["minutes"] != 1 {
		t.Errorf("пляж B: %+v", b.Value)
	}
	if m := store.saved["2/"+domain.WalkToMetroKey][0]; m.Value["distance_m"] != 140.0 || m.Value["minutes"] != 2 {
		t.Errorf("метро B: %+v", m.Value)
	}
	// Отель C вне графа: фактов и вызовов сохранения нет.
	if _, ok := store.saved["3/"+domain.WalkToMetroKey]; ok {
		t.Error("отель вне графа получил запись")
	}
}

func TestRoutePathThinsLongPaths(t *testing.T) {
	var line orb.LineString
	for i := 0; i < 1000; i++ {
		line = append(line, orb.Point{2.1 + float64(i)*1e-5, 41.4})
	}
	r := domain.WalkRoute{Nodes: []int64{1}, Edges: []int64{7}}
	path := routePath(r, map[int64]domain.WalkEdge{7: {ID: 7, Source: 1}}, map[int64]orb.LineString{7: line})
	if len(path) > maxPathPoints+1 || len(path) < 100 {
		t.Fatalf("точек %d", len(path))
	}
	if path[0] != round5(line[0]) || path[len(path)-1] != round5(line[len(line)-1]) {
		t.Error("начало и конец пути должны сохраниться")
	}
	// Ребро, пройденное от Target к Source, разворачивается.
	rev := routePath(domain.WalkRoute{Nodes: []int64{2}, Edges: []int64{7}},
		map[int64]domain.WalkEdge{7: {ID: 7, Source: 1, Target: 2}}, map[int64]orb.LineString{7: {{1, 1}, {2, 2}}})
	if len(rev) != 2 || rev[0] != [2]float64{2, 2} {
		t.Errorf("разворот: %v", rev)
	}
}

package domain

import (
	"math"
	"testing"

	"github.com/paulmach/orb"
)

func TestBuildWalkEdges(t *testing.T) {
	// Путь A: узлы 1-2-3-4, путь B: 3-5. Узел 3 — перекрёсток: путь A режется на 1-3 (через 2) и 3-4.
	a := WalkWay{OSMID: 10, Nodes: []int64{1, 2, 3, 4},
		Points: []orb.Point{{2.17, 41.38}, {2.171, 41.38}, {2.172, 41.38}, {2.173, 41.38}}}
	b := WalkWay{OSMID: 11, Nodes: []int64{3, 5}, Points: []orb.Point{{2.172, 41.38}, {2.172, 41.381}}}
	edges := BuildWalkEdges([]WalkWay{a, b})
	if len(edges) != 3 {
		t.Fatalf("рёбер %d, want 3: %+v", len(edges), edges)
	}
	if edges[0].Source != 1 || edges[0].Target != 3 || len(edges[0].Geom) != 3 {
		t.Errorf("первое ребро должно идти 1→3 через узел 2: %+v", edges[0])
	}
	// 0,002° по долготе на широте 41,38° ≈ 167 м.
	if math.Abs(edges[0].LengthM-167) > 3 {
		t.Errorf("длина %v м, want ≈167", edges[0].LengthM)
	}
	if edges[1].Source != 3 || edges[1].Target != 4 || edges[2].Source != 3 || edges[2].Target != 5 {
		t.Errorf("рёбра: %+v", edges[1:])
	}

	// Путь с несовпадающими узлами и точками и петля в один узел игнорируются.
	bad := WalkWay{Nodes: []int64{1, 2}, Points: []orb.Point{{0, 0}}}
	loop := WalkWay{Nodes: []int64{7, 7}, Points: []orb.Point{{0, 0}, {0, 0}}}
	if got := BuildWalkEdges([]WalkWay{bad, loop}); len(got) != 0 {
		t.Errorf("рёбер %d, want 0", len(got))
	}
}

// Граф: 1 —100— 2 —100— 3 —100— 4, плюс обход 1 —250— 4.
func testGraph() *WalkGraph {
	return NewWalkGraph([]WalkEdge{
		{ID: 1, Source: 1, Target: 2, LengthM: 100},
		{ID: 2, Source: 2, Target: 3, LengthM: 100},
		{ID: 3, Source: 3, Target: 4, LengthM: 100},
		{ID: 4, Source: 1, Target: 4, LengthM: 250},
	})
}

func TestShortestToAny(t *testing.T) {
	g := testGraph()
	// Старт на ребре 1–2 в 30 м от узла 1 и 70 м от узла 2; цель на ребре 3–4 в 20 м от узла 3 и 80 м от узла 4.
	start := WalkSnap{EdgeID: 1, Source: 1, Target: 2, ToSourceM: 30, ToTargetM: 70}
	target := WalkTarget{ID: 9, Name: "метро", Snap: WalkSnap{EdgeID: 3, Source: 3, Target: 4, ToSourceM: 20, ToTargetM: 80}}
	r, ok := g.ShortestToAny(start, []WalkTarget{target}, 1000)
	if !ok {
		t.Fatal("маршрут не найден")
	}
	// Через узлы 2 и 3: 70 + 100 + 20 = 190 м (через узел 1 и 4: 30 + 250 + 80 = 360).
	if r.DistM != 190 || r.StartExtra != 70 || r.EndExtra != 20 {
		t.Errorf("маршрут: %+v", r)
	}
	if len(r.Nodes) != 2 || r.Nodes[0] != 2 || r.Nodes[1] != 3 || len(r.Edges) != 1 || r.Edges[0] != 2 {
		t.Errorf("узлы %v, рёбра %v", r.Nodes, r.Edges)
	}
	if r.Minutes() != 3 { // 190 / 80 = 2,375 → 3
		t.Errorf("минут %d, want 3", r.Minutes())
	}
}

func TestShortestToAnyPicksNearestTarget(t *testing.T) {
	g := testGraph()
	start := WalkSnap{EdgeID: 1, Source: 1, Target: 2, ToSourceM: 0, ToTargetM: 100}
	far := WalkTarget{ID: 1, Snap: WalkSnap{Source: 3, Target: 4, ToSourceM: 0, ToTargetM: 100}}
	near := WalkTarget{ID: 2, Snap: WalkSnap{Source: 2, Target: 3, ToSourceM: 10, ToTargetM: 90}}
	r, ok := g.ShortestToAny(start, []WalkTarget{far, near}, 1000)
	if !ok || r.Target.ID != 2 || r.DistM != 110 {
		t.Errorf("%+v, %v: ближайшая цель — вторая, 100 + 10 м", r, ok)
	}
}

func TestShortestToAnyLimitsAndMissing(t *testing.T) {
	g := testGraph()
	start := WalkSnap{Source: 1, Target: 2, ToSourceM: 0, ToTargetM: 100}
	target := WalkTarget{Snap: WalkSnap{Source: 3, Target: 4, ToSourceM: 0, ToTargetM: 100}}
	if _, ok := g.ShortestToAny(start, []WalkTarget{target}, 150); ok {
		t.Error("маршрут длиннее предела вернулся")
	}
	// Цель вне графа и пустой список целей.
	ghost := WalkTarget{Snap: WalkSnap{Source: 98, Target: 99}}
	if _, ok := g.ShortestToAny(start, []WalkTarget{ghost}, 1000); ok {
		t.Error("цель вне графа найдена")
	}
	if _, ok := g.ShortestToAny(start, nil, 1000); ok {
		t.Error("пустой список целей дал маршрут")
	}
	// Старт и цель на одном ребре — через общий узел.
	same := WalkTarget{Snap: WalkSnap{Source: 1, Target: 2, ToSourceM: 5, ToTargetM: 95}}
	r, ok := g.ShortestToAny(WalkSnap{Source: 1, Target: 2, ToSourceM: 30, ToTargetM: 70}, []WalkTarget{same}, 1000)
	if !ok || r.DistM != 35 {
		t.Errorf("%+v, %v: 30 + 5 через узел 1", r, ok)
	}
}

func TestSampleBoundary(t *testing.T) {
	// ≈ 800 м вдоль широты: с шагом 40 м ровно столько точек, сколько шагов, плюс начало.
	ls := orb.LineString{}
	for i := 0; i <= 80; i++ {
		ls = append(ls, orb.Point{2.17 + float64(i)*0.0001, 41.38}) // ≈ 8,3 м на шаг
	}
	pts := SampleBoundary(ls, WalkBeachSampleM)
	if len(pts) < 15 || len(pts) > 25 || pts[0] != ls[0] || pts[len(pts)-1] != ls[len(ls)-1] {
		t.Errorf("точек %d, начало и конец должны сохраниться", len(pts))
	}
	if len(SampleBoundary(nil, 40)) != 0 {
		t.Error("пустая линия дала точки")
	}
}

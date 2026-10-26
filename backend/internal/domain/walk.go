package domain

import (
	"container/heap"
	"math"

	"github.com/paulmach/orb"
)

// Параметры расчёта пеших маршрутов (метод WalkMethod).
const (
	WalkMethod = "walk_route@1"
	// WalkSpeedMPerMin — скорость пешехода, 80 м/мин (4,8 км/ч): рельеф не учитывается.
	WalkSpeedMPerMin = 80.0
	// WalkSnapMaxM — дальше этого расстояния до ближайшего пути точка (отель, метро, пляж) не привязывается к графу.
	WalkSnapMaxM = 200.0
	// WalkBeachSampleM — шаг точек вдоль границы пляжа.
	WalkBeachSampleM = 40.0
)

// Характеристики пеших маршрутов, их цели и предельные длины маршрута.
const (
	WalkToBeachKey = "walk_to_beach"
	WalkToMetroKey = "walk_to_metro"

	DestBeach = "beach"
	DestMetro = "metro"

	// BeachMaxFromCoastM — песок и пляжи дальше этого расстояния от береговой линии пляжем не считаются.
	BeachMaxFromCoastM = 120.0

	WalkBeachMaxM = 4000.0
	WalkMetroMaxM = 2500.0
)

// Destination — цель пеших маршрутов: вход в метро или точка границы пляжа.
type Destination struct {
	OSMID int64
	Kind  string // DestMetro | DestBeach
	Name  string
	Point orb.Point
}

// HotelPoint — отель как начало маршрута.
type HotelPoint struct {
	ID    int64
	Name  string
	Point orb.Point
}

// WalkWay — путь OSM: узлы и их координаты.
type WalkWay struct {
	OSMID  int64
	Nodes  []int64
	Points []orb.Point
}

// WalkEdge — ребро графа между перекрёстками; Geom повторяет изгибы пути, Source и Target — id узлов OSM.
type WalkEdge struct {
	ID      int64
	Source  int64
	Target  int64
	LengthM float64
	Geom    orb.LineString
}

// BuildWalkEdges режет пути на рёбра между перекрёстками: узел делит путь, если встречается в двух и более
// путях или в нескольких местах одного пути (петли), и на концах пути. Цепочки узлов степени 2 схлопываются в одно ребро.
func BuildWalkEdges(ways []WalkWay) []WalkEdge {
	uses := map[int64]int{}
	for _, w := range ways {
		if len(w.Nodes) != len(w.Points) || len(w.Nodes) < 2 {
			continue
		}
		for _, n := range w.Nodes {
			uses[n]++
		}
	}
	var edges []WalkEdge
	for _, w := range ways {
		if len(w.Nodes) != len(w.Points) || len(w.Nodes) < 2 {
			continue
		}
		start := 0
		for i := 1; i < len(w.Nodes); i++ {
			if i != len(w.Nodes)-1 && uses[w.Nodes[i]] < 2 {
				continue
			}
			geom := orb.LineString(append([]orb.Point(nil), w.Points[start:i+1]...))
			if w.Nodes[start] != w.Nodes[i] {
				edges = append(edges, WalkEdge{Source: w.Nodes[start], Target: w.Nodes[i], LengthM: lengthM(geom), Geom: geom})
			}
			start = i
		}
	}
	return edges
}

// lengthM — длина линии в метрах по формуле гаверсинусов.
func lengthM(ls orb.LineString) float64 {
	var sum float64
	for i := 1; i < len(ls); i++ {
		sum += haversineM(ls[i-1], ls[i])
	}
	return sum
}

func haversineM(a, b orb.Point) float64 {
	const r = 6371008.8
	la1, la2 := a[1]*math.Pi/180, b[1]*math.Pi/180
	dLat, dLon := la2-la1, (b[0]-a[0])*math.Pi/180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

// WalkSnap — привязка точки к ближайшему ребру графа: расстояние до ребра и вдоль ребра до его концов.
type WalkSnap struct {
	EdgeID    int64
	Source    int64
	Target    int64
	ToSourceM float64 // от точки до узла Source: до ребра по прямой плюс вдоль ребра
	ToTargetM float64
}

// WalkTarget — цель маршрута: точка, привязанная к графу.
type WalkTarget struct {
	ID   int64
	Name string
	Snap WalkSnap
}

// WalkRoute — найденный маршрут. Nodes — цепочка узлов, Edges — рёбра между ними.
type WalkRoute struct {
	DistM      float64
	StartExtra float64 // до первого узла (по прямой до ребра и вдоль него)
	EndExtra   float64 // от последнего узла до цели
	Target     WalkTarget
	Nodes      []int64
	Edges      []int64
}

// Minutes — время в пути, округлённое вверх до целых минут.
func (r WalkRoute) Minutes() int { return r.MinutesAt(WalkSpeedMPerMin) }

// MinutesAt — время маршрута в минутах (с округлением вверх) при скорости speedMPerMin.
func (r WalkRoute) MinutesAt(speedMPerMin float64) int { return int(math.Ceil(r.DistM / speedMPerMin)) }

// WalkGraph — неориентированный граф пешеходных рёбер для Дейкстры.
type WalkGraph struct {
	ids   map[int64]int32 // id узла OSM → плотный индекс
	nodes []int64
	adj   [][]walkArc
}

type walkArc struct {
	to   int32
	len  float32
	edge int64
}

// NewWalkGraph строит граф из рёбер.
func NewWalkGraph(edges []WalkEdge) *WalkGraph {
	g := &WalkGraph{ids: map[int64]int32{}}
	idx := func(n int64) int32 {
		if i, ok := g.ids[n]; ok {
			return i
		}
		i := int32(len(g.nodes))
		g.ids[n] = i
		g.nodes = append(g.nodes, n)
		g.adj = append(g.adj, nil)
		return i
	}
	for _, e := range edges {
		a, b := idx(e.Source), idx(e.Target)
		g.adj[a] = append(g.adj[a], walkArc{to: b, len: float32(e.LengthM), edge: e.ID})
		g.adj[b] = append(g.adj[b], walkArc{to: a, len: float32(e.LengthM), edge: e.ID})
	}
	return g
}

// Has сообщает, есть ли узел в графе.
func (g *WalkGraph) Has(node int64) bool { _, ok := g.ids[node]; return ok }

type pqItem struct {
	node int32
	dist float64
}

type pq []pqItem

func (q pq) Len() int           { return len(q) }
func (q pq) Less(i, j int) bool { return q[i].dist < q[j].dist }
func (q pq) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *pq) Push(x any)        { *q = append(*q, x.(pqItem)) }
func (q *pq) Pop() any          { old := *q; n := len(old); it := old[n-1]; *q = old[:n-1]; return it }

// ShortestToAny ищет самый короткий путь от привязанной точки start до ближайшей из целей targets (Дейкстра с
// несколькими стартами и концами). Маршрут длиннее maxM не возвращается. Второй результат false — маршрута нет.
func (g *WalkGraph) ShortestToAny(start WalkSnap, targets []WalkTarget, maxM float64) (WalkRoute, bool) {
	type endCand struct {
		extra  float64
		target int
	}
	ends := map[int32][]endCand{}
	for i, t := range targets {
		for _, c := range []struct {
			node  int64
			extra float64
		}{{t.Snap.Source, t.Snap.ToSourceM}, {t.Snap.Target, t.Snap.ToTargetM}} {
			if n, ok := g.ids[c.node]; ok {
				ends[n] = append(ends[n], endCand{c.extra, i})
			}
		}
	}
	dist := map[int32]float64{}
	prev := map[int32]walkArc{} // дуга, по которой пришли; to хранит предыдущий узел
	startExtra := map[int32]float64{}
	q := &pq{}
	for _, c := range []struct {
		node  int64
		extra float64
	}{{start.Source, start.ToSourceM}, {start.Target, start.ToTargetM}} {
		n, ok := g.ids[c.node]
		if !ok {
			continue
		}
		if d, seen := dist[n]; !seen || c.extra < d {
			dist[n], startExtra[n] = c.extra, c.extra
			heap.Push(q, pqItem{n, c.extra})
		}
	}

	best := maxM
	bestNode, bestTarget, bestEnd := int32(-1), -1, 0.0
	for q.Len() > 0 {
		it := heap.Pop(q).(pqItem)
		if it.dist > dist[it.node] {
			continue
		}
		if it.dist >= best {
			break
		}
		for _, c := range ends[it.node] {
			if total := it.dist + c.extra; total < best {
				best, bestNode, bestTarget, bestEnd = total, it.node, c.target, c.extra
			}
		}
		for _, a := range g.adj[it.node] {
			nd := it.dist + float64(a.len)
			if nd >= best {
				continue
			}
			if d, seen := dist[a.to]; !seen || nd < d {
				dist[a.to] = nd
				prev[a.to] = walkArc{to: it.node, edge: a.edge}
				heap.Push(q, pqItem{a.to, nd})
			}
		}
	}
	if bestNode < 0 {
		return WalkRoute{}, false
	}
	route := WalkRoute{DistM: best, EndExtra: bestEnd, Target: targets[bestTarget]}
	var nodes []int64
	var edges []int64
	for n := bestNode; ; {
		nodes = append(nodes, g.nodes[n])
		p, ok := prev[n]
		if !ok {
			route.StartExtra = startExtra[n]
			break
		}
		edges = append(edges, p.edge)
		n = p.to
	}
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
	for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
		edges[i], edges[j] = edges[j], edges[i]
	}
	route.Nodes, route.Edges = nodes, edges
	return route, true
}

// SampleBoundary возвращает точки вдоль линии с шагом stepM (включая начало); для коротких линий — начало и конец.
func SampleBoundary(ls orb.LineString, stepM float64) []orb.Point {
	if len(ls) == 0 {
		return nil
	}
	out := []orb.Point{ls[0]}
	var acc float64
	for i := 1; i < len(ls); i++ {
		acc += haversineM(ls[i-1], ls[i])
		if acc >= stepM {
			out = append(out, ls[i])
			acc = 0
		}
	}
	if last := ls[len(ls)-1]; out[len(out)-1] != last {
		out = append(out, last)
	}
	return out
}

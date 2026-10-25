package compute

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// walkTTL — срок годности: пешие пути и входы в метро в OSM меняются медленно, но граф неполон.
const walkTTL = 180 * 24 * time.Hour

// maxPathPoints — сколько точек пути сохраняется в evidence (для карты); длинный путь прореживается.
const maxPathPoints = 150

// WalkStore — хранилище для расчёта пеших маршрутов.
type WalkStore interface {
	LoadWalkEdges(ctx context.Context) ([]domain.WalkEdge, error)
	HotelPoints(ctx context.Context) ([]domain.HotelPoint, error)
	Destinations(ctx context.Context, kind string) ([]domain.Destination, error)
	// SnapPoints привязывает точки к ближайшему ребру графа; ok[i] = false, если рядом нет рёбер.
	SnapPoints(ctx context.Context, points []orb.Point, maxM float64) (snaps []domain.WalkSnap, ok []bool, err error)
	EdgeGeoms(ctx context.Context, ids []int64) (map[int64]orb.LineString, error)
	// SaveFacts заменяет активные факты характеристики key у отеля (пустой список — только снять прежние).
	SaveFacts(ctx context.Context, hotelID int64, key string, facts []domain.Fact) error
}

// Walk считает walk_to_beach и walk_to_metro: кратчайший пеший путь по графу OSM от отеля до ближайшей цели.
type Walk struct {
	Store WalkStore
	Now   func() time.Time // для тестов; по умолчанию time.Now
}

// WalkResult — сколько отелей получили маршрут.
type WalkResult struct {
	Hotels       int // отелей, привязанных к графу
	Beach, Metro int // отелей с найденным маршрутом
	Unsnapped    int // отелей дальше WalkSnapMaxM от любого пути
	BeachTargets int
	MetroTargets int
}

type walkKind struct {
	key, dest string
	maxM      float64
}

var walkKinds = []walkKind{
	{domain.WalkToBeachKey, domain.DestBeach, domain.WalkBeachMaxM},
	{domain.WalkToMetroKey, domain.DestMetro, domain.WalkMetroMaxM},
}

// Run пересчитывает пешие маршруты у всех отелей.
func (c *Walk) Run(ctx context.Context) (WalkResult, error) {
	var res WalkResult
	edges, err := c.Store.LoadWalkEdges(ctx)
	if err != nil {
		return res, fmt.Errorf("граф: %w", err)
	}
	if len(edges) == 0 {
		return res, fmt.Errorf("пешеходный граф пуст: сначала collect walk-data")
	}
	graph := domain.NewWalkGraph(edges)
	edgeByID := make(map[int64]domain.WalkEdge, len(edges))
	for _, e := range edges {
		edgeByID[e.ID] = e
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}

	hotels, err := c.Store.HotelPoints(ctx)
	if err != nil {
		return res, fmt.Errorf("отели: %w", err)
	}
	hotelPts := make([]orb.Point, len(hotels))
	for i, h := range hotels {
		hotelPts[i] = h.Point
	}
	hotelSnaps, hotelOK, err := c.Store.SnapPoints(ctx, hotelPts, domain.WalkSnapMaxM)
	if err != nil {
		return res, fmt.Errorf("привязка отелей: %w", err)
	}

	targets := map[string][]domain.WalkTarget{}
	for _, k := range walkKinds {
		dests, err := c.Store.Destinations(ctx, k.dest)
		if err != nil {
			return res, fmt.Errorf("цели %s: %w", k.dest, err)
		}
		pts := make([]orb.Point, len(dests))
		for i, d := range dests {
			pts[i] = d.Point
		}
		snaps, ok, err := c.Store.SnapPoints(ctx, pts, domain.WalkSnapMaxM)
		if err != nil {
			return res, fmt.Errorf("привязка целей %s: %w", k.dest, err)
		}
		for i, d := range dests {
			if ok[i] {
				targets[k.dest] = append(targets[k.dest], domain.WalkTarget{ID: d.OSMID, Name: d.Name, Snap: snaps[i]})
			}
		}
		log.Printf("walk: целей %s привязано %d из %d", k.dest, len(targets[k.dest]), len(dests))
	}
	res.BeachTargets, res.MetroTargets = len(targets[domain.DestBeach]), len(targets[domain.DestMetro])

	for i, h := range hotels {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if !hotelOK[i] {
			res.Unsnapped++
			continue
		}
		res.Hotels++
		for _, k := range walkKinds {
			var facts []domain.Fact
			if route, found := graph.ShortestToAny(hotelSnaps[i], targets[k.dest], k.maxM); found {
				f, err := c.walkFact(ctx, h, k, route, edgeByID, len(edges), now)
				if err != nil {
					return res, fmt.Errorf("отель %d, %s: %w", h.ID, k.key, err)
				}
				facts = []domain.Fact{f}
				if k.dest == domain.DestBeach {
					res.Beach++
				} else {
					res.Metro++
				}
			}
			if err := c.Store.SaveFacts(ctx, h.ID, k.key, facts); err != nil {
				return res, fmt.Errorf("отель %d, %s: %w", h.ID, k.key, err)
			}
		}
		if (i+1)%50 == 0 {
			log.Printf("walk: %d из %d отелей", i+1, len(hotels))
		}
	}
	return res, nil
}

func (c *Walk) walkFact(ctx context.Context, h domain.HotelPoint, k walkKind, r domain.WalkRoute,
	edgeByID map[int64]domain.WalkEdge, graphEdges int, now time.Time) (domain.Fact, error) {
	geoms, err := c.Store.EdgeGeoms(ctx, r.Edges)
	if err != nil {
		return domain.Fact{}, err
	}
	path := routePath(r, edgeByID, geoms)
	until := now.Add(walkTTL)
	value := map[string]any{
		"minutes":     r.Minutes(),
		"distance_m":  math.Round(r.DistM),
		"target_kind": k.dest,
	}
	if r.Target.Name != "" {
		value["target"] = r.Target.Name
	}
	id := fmt.Sprint(h.ID)
	details := map[string]any{
		"target_osm_id":      r.Target.ID,
		"start_access_m":     math.Round(r.StartExtra),
		"end_access_m":       math.Round(r.EndExtra),
		"graph_nodes":        len(r.Nodes),
		"graph_edges_total":  graphEdges,
		"path":               path,
		"independence_group": domain.WalkGroup,
		"params": map[string]any{
			"speed_m_per_min": domain.WalkSpeedMPerMin, "snap_max_m": domain.WalkSnapMaxM, "max_route_m": k.maxM,
		},
		"assumptions": []string{
			"рельеф не учитывается: перепад высот на маршруте не считается",
			"переходы улиц, светофоры и ожидание не учитываются",
			"отель — точка OSM, реальный вход неизвестен; до ближайшего пути идём по прямой",
			"цель пляжа — ближайшая точка границы пляжа OSM, метро — ближайший вход или станция OSM",
			"граф пешеходных путей OSM может быть неполон",
		},
	}
	return domain.Fact{
		EntityType: domain.EntityHotel, EntityID: id, Key: k.key, Value: value,
		Source: domain.SourceOSM, Method: domain.WalkMethod, Confidence: domain.WalkReliability,
		ObservedAt: now, ValidUntil: &until,
		Evidence: []domain.Evidence{{
			EntityType: domain.EntityHotel, EntityID: id, Key: k.key, Claimed: value,
			Source: domain.SourceOSM, Method: domain.WalkMethod, AuthorType: domain.AuthorTool, AuthorID: domain.WalkMethod,
			Reliability: domain.WalkReliability, ObservedAt: now, ValidUntil: &until, Details: details,
		}},
	}, nil
}

// routePath склеивает геометрии рёбер маршрута в одну линию [lon, lat] с точностью до 5 знаков (≈1 м);
// длинный путь прореживается до maxPathPoints точек, начало и конец сохраняются.
func routePath(r domain.WalkRoute, edgeByID map[int64]domain.WalkEdge, geoms map[int64]orb.LineString) [][2]float64 {
	var line []orb.Point
	for i, eid := range r.Edges {
		g := geoms[eid]
		if len(g) == 0 || i >= len(r.Nodes) {
			continue
		}
		pts := append([]orb.Point(nil), g...)
		if edgeByID[eid].Source != r.Nodes[i] {
			for a, b := 0, len(pts)-1; a < b; a, b = a+1, b-1 {
				pts[a], pts[b] = pts[b], pts[a]
			}
		}
		if len(line) > 0 {
			pts = pts[1:] // общий узел уже есть
		}
		line = append(line, pts...)
	}
	step := 1
	if len(line) > maxPathPoints {
		step = (len(line) + maxPathPoints - 1) / maxPathPoints
	}
	out := make([][2]float64, 0, min(len(line), maxPathPoints+1))
	for i := 0; i < len(line); i += step {
		out = append(out, round5(line[i]))
	}
	if n := len(line); n > 0 && (n-1)%step != 0 {
		out = append(out, round5(line[n-1]))
	}
	return out
}

func round5(p orb.Point) [2]float64 {
	return [2]float64{math.Round(p[0]*1e5) / 1e5, math.Round(p[1]*1e5) / 1e5}
}

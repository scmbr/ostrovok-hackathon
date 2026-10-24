package domain

import (
	"math"

	"github.com/paulmach/orb"
)

// Параметры расчёта фасадов (этап 2).
const (
	// MinFacadeM — наименьшая длина фасада по внешнему контуру.
	MinFacadeM = 3.0
	// MinCourtyardFacadeM — наименьшая длина фасада по контуру двора.
	MinCourtyardFacadeM = 8.0
	// MinLightWellAreaM2 — двор площадью меньше считается световым колодцем, окон на него не берём.
	MinLightWellAreaM2 = 30.0
	// BlindWallDistM — отрезок контура ближе к соседнему зданию считается глухой стеной.
	BlindWallDistM = 1.5
	// BlindFractionMin — доля длины отрезка у соседа, начиная с которой отрезок глухой.
	BlindFractionMin = 0.5
	// StreetProbeM — дальность луча от фасада до дороги: дальше фасад считается дворовым.
	StreetProbeM = 25.0
	// RoadsRadiusM — дороги грузим только вокруг отелей.
	RoadsRadiusM = 300

	minEdgeM    = 0.05 // короче — артефакт контура
	maxTurnDeg  = 10.0 // соседние рёбра с поворотом меньше склеиваются в один фасад
	maxDriftDeg = 20.0 // и отклонение от первого ребра склейки не больше
)

// Edge — ребро контура здания в порядке обхода: здание остаётся слева,
// внешняя нормаль — справа от направления A→B. Ребра одного кольца идут подряд и имеют общий Ring.
type Edge struct {
	A, B      orb.Point // (lon, lat)
	Courtyard bool      // ребро внутреннего кольца
	Ring      int
}

// Facade — сторона здания отеля. AzimuthDeg — куда смотрят окна (внешняя нормаль, от севера по часовой).
type Facade struct {
	HotelID     int64
	AzimuthDeg  float64
	Geom        orb.LineString
	LengthM     float64
	FacesStreet bool
	Courtyard   bool
}

// Probe — луч от середины фасада наружу для поиска дороги.
type Probe struct {
	Mid        orb.Point
	AzimuthDeg float64
}

// Probe возвращает луч от середины фасада.
func (f Facade) Probe() Probe {
	a, b := f.Geom[0], f.Geom[len(f.Geom)-1]
	return Probe{Mid: orb.Point{(a[0] + b[0]) / 2, (a[1] + b[1]) / 2}, AzimuthDeg: f.AzimuthDeg}
}

// HotelBuilding — отель и контур его основного здания.
type HotelBuilding struct {
	HotelID    int64
	BuildingID int64
	Geom       orb.MultiPolygon
}

// ContourStats — сколько рёбер контура рассматривалось и сколько отброшено как глухие стены.
type ContourStats struct {
	Segments int
	Blind    int
}

// plane — локальная плоская проекция в метрах вокруг точки; для масштаба здания хватает с запасом.
type plane struct {
	lon0, lat0 float64
	kx, ky     float64
}

func newPlane(origin orb.Point) plane {
	const metersPerDegree = math.Pi / 180 * 6371008.8
	return plane{
		lon0: origin[0], lat0: origin[1],
		kx: metersPerDegree * math.Cos(origin[1]*math.Pi/180), ky: metersPerDegree,
	}
}

func (p plane) xy(pt orb.Point) (x, y float64) {
	return (pt[0] - p.lon0) * p.kx, (pt[1] - p.lat0) * p.ky
}

// ContourEdges разбивает контуры на ориентированные рёбра. Внешние кольца обходятся против часовой
// стрелки, внутренние — по часовой, поэтому внешняя нормаль всегда справа. Дворы площадью меньше
// MinLightWellAreaM2 (световые колодцы) пропускаются целиком.
func ContourEdges(mp orb.MultiPolygon) []Edge {
	var edges []Edge
	ringID := 0
	for _, poly := range mp {
		for r, ring := range poly {
			pts := openRing(ring)
			if len(pts) < 3 {
				continue
			}
			pl := newPlane(pts[0])
			area := signedArea(pl, pts)
			courtyard := r > 0
			if courtyard && math.Abs(area) < MinLightWellAreaM2 {
				continue
			}
			if (!courtyard && area < 0) || (courtyard && area > 0) {
				reverse(pts)
			}
			ringID++
			for i := range pts {
				a, b := pts[i], pts[(i+1)%len(pts)]
				ax, ay := pl.xy(a)
				bx, by := pl.xy(b)
				if math.Hypot(bx-ax, by-ay) < minEdgeM {
					continue
				}
				edges = append(edges, Edge{A: a, B: b, Courtyard: courtyard, Ring: ringID})
			}
		}
	}
	return edges
}

// BuildFacades склеивает подряд идущие почти прямые не глухие рёбра в фасады. blind[i] — ребро i
// глухое (стена соседа) и фасадом не становится. Слишком короткие фасады отбрасываются.
// FacesStreet и HotelID вызывающий заполняет сам.
func BuildFacades(edges []Edge, blind []bool) []Facade {
	var out []Facade
	for from := 0; from < len(edges); {
		to := from
		for to < len(edges) && edges[to].Ring == edges[from].Ring {
			to++
		}
		out = append(out, ringFacades(edges[from:to], blind[from:to])...)
		from = to
	}
	return out
}

func ringFacades(edges []Edge, blind []bool) []Facade {
	n := len(edges)
	pl := newPlane(edges[0].A)
	dir := make([]float64, n) // направление ребра, градусы
	vec := make([][2]float64, n)
	for i, e := range edges {
		ax, ay := pl.xy(e.A)
		bx, by := pl.xy(e.B)
		vec[i] = [2]float64{bx - ax, by - ay}
		dir[i] = bearing(vec[i][0], vec[i][1])
	}

	// Начинаем с ребра, перед которым склейка заведомо обрывается: иначе фасад, пересекающий начало кольца, развалится.
	start := 0
	for i := 0; i < n; i++ {
		prev := (i + n - 1) % n
		if blind[prev] || angleDiff(dir[prev], dir[i]) > maxTurnDeg {
			start = i
			break
		}
	}

	var out []Facade
	var run []int
	flush := func() {
		if len(run) == 0 {
			return
		}
		if f, ok := makeFacade(edges, vec, run); ok {
			out = append(out, f)
		}
		run = run[:0]
	}
	for j := 0; j < n; j++ {
		i := (start + j) % n
		if blind[i] {
			flush()
			continue
		}
		if len(run) > 0 {
			last := run[len(run)-1]
			if angleDiff(dir[last], dir[i]) > maxTurnDeg || angleDiff(dir[run[0]], dir[i]) > maxDriftDeg {
				flush()
			}
		}
		run = append(run, i)
	}
	flush()
	return out
}

func makeFacade(edges []Edge, vec [][2]float64, run []int) (Facade, bool) {
	var cx, cy, length float64
	geom := orb.LineString{edges[run[0]].A}
	for _, i := range run {
		cx += vec[i][0]
		cy += vec[i][1]
		length += math.Hypot(vec[i][0], vec[i][1])
		geom = append(geom, edges[i].B)
	}
	minLen := MinFacadeM
	if edges[run[0]].Courtyard {
		minLen = MinCourtyardFacadeM
	}
	if length < minLen || math.Hypot(cx, cy) < 0.5*minLen {
		return Facade{}, false
	}
	// Внешняя нормаль — справа от хорды (cx, cy).
	return Facade{
		AzimuthDeg: bearing(cy, -cx),
		Geom:       geom,
		LengthM:    length,
		Courtyard:  edges[run[0]].Courtyard,
	}, true
}

// bearing — азимут вектора (east, north) в градусах от севера по часовой, [0, 360).
func bearing(east, north float64) float64 {
	a := math.Atan2(east, north) * 180 / math.Pi
	if a < 0 {
		a += 360
	}
	return a
}

// angleDiff — наименьшая разность азимутов, [0, 180].
func angleDiff(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 180 {
		d = 360 - d
	}
	return d
}

func openRing(r orb.Ring) []orb.Point {
	pts := append([]orb.Point(nil), r...)
	if len(pts) > 1 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	return pts
}

// signedArea — площадь в м², положительная при обходе против часовой стрелки.
func signedArea(pl plane, pts []orb.Point) float64 {
	var s float64
	for i := range pts {
		x1, y1 := pl.xy(pts[i])
		x2, y2 := pl.xy(pts[(i+1)%len(pts)])
		s += x1*y2 - x2*y1
	}
	return s / 2
}

func reverse(pts []orb.Point) {
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
}

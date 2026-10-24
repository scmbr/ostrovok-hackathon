package domain

import (
	"math"
	"sort"
	"testing"

	"github.com/paulmach/orb"
)

// testPlane — начало координат в Барселоне; xy-метры переводятся в lon/lat обратным преобразованием.
var testPlane = newPlane(orb.Point{2.19, 41.39})

func pt(x, y float64) orb.Point {
	return orb.Point{testPlane.lon0 + x/testPlane.kx, testPlane.lat0 + y/testPlane.ky}
}

// ring строит замкнутое кольцо из точек в метрах.
func ring(xy ...float64) orb.Ring {
	r := make(orb.Ring, 0, len(xy)/2+1)
	for i := 0; i < len(xy); i += 2 {
		r = append(r, pt(xy[i], xy[i+1]))
	}
	return append(r, r[0])
}

// facadesOf считает фасады, считая глухими рёбра, для которых blindIf вернула true.
func facadesOf(mp orb.MultiPolygon, blindIf func(Edge) bool) ([]Facade, ContourStats) {
	edges := ContourEdges(mp)
	blind := make([]bool, len(edges))
	st := ContourStats{Segments: len(edges)}
	for i, e := range edges {
		if blindIf != nil && blindIf(e) {
			blind[i] = true
			st.Blind++
		}
	}
	return BuildFacades(edges, blind), st
}

func azimuths(fs []Facade) []int {
	var out []int
	for _, f := range fs {
		out = append(out, int(math.Round(f.AzimuthDeg))%360)
	}
	sort.Ints(out)
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Прямоугольник 20 × 10 м: север/юг по 20 м, запад/восток по 10 м.
var box = orb.MultiPolygon{{ring(0, 0, 20, 0, 20, 10, 0, 10)}}

func TestFacadesOfBox(t *testing.T) {
	fs, st := facadesOf(box, nil)
	if want := []int{0, 90, 180, 270}; !equalInts(azimuths(fs), want) {
		t.Errorf("азимуты %v, want %v", azimuths(fs), want)
	}
	if st.Segments != 4 || st.Blind != 0 {
		t.Errorf("stats %+v", st)
	}
}

func TestFacadesOrientationDoesNotMatter(t *testing.T) {
	cw := orb.MultiPolygon{{ring(0, 0, 0, 10, 20, 10, 20, 0)}} // по часовой стрелке
	fs, _ := facadesOf(cw, nil)
	if want := []int{0, 90, 180, 270}; !equalInts(azimuths(fs), want) {
		t.Errorf("азимуты %v, want %v", azimuths(fs), want)
	}
}

// Здание зажато соседями слева и справа: торцы — глухие стены, остаются фасад на север и на юг.
func TestFacadesBetweenTwoNeighbours(t *testing.T) {
	west, east := pt(0, 5)[0], pt(20, 5)[0]
	nearX := func(lon, target float64) bool { return math.Abs(lon-target) < 1e-9 }
	fs, st := facadesOf(box, func(e Edge) bool {
		return (nearX(e.A[0], west) && nearX(e.B[0], west)) || (nearX(e.A[0], east) && nearX(e.B[0], east))
	})
	if want := []int{0, 180}; !equalInts(azimuths(fs), want) {
		t.Errorf("азимуты %v, want %v", azimuths(fs), want)
	}
	if st.Segments != 4 || st.Blind != 2 {
		t.Errorf("stats %+v, want 4 сегмента из них 2 глухих", st)
	}
	for _, f := range fs {
		if math.Abs(f.LengthM-20) > 0.1 {
			t.Errorf("длина фасада %v, want 20", f.LengthM)
		}
	}
}

func TestFacadesMergeCollinear(t *testing.T) {
	withMidpoint := orb.MultiPolygon{{ring(0, 0, 10, 0, 20, 0, 20, 10, 0, 10)}}
	fs, _ := facadesOf(withMidpoint, nil)
	if len(fs) != 4 {
		t.Fatalf("фасадов %d, want 4 (вершина на прямой не делит фасад)", len(fs))
	}
}

func TestCourtyardFacades(t *testing.T) {
	// Дом 30 × 30 м, двор 10 × 10 м (100 м²) и световой колодец 4 × 4 м (16 м²).
	building := orb.MultiPolygon{{
		ring(0, 0, 30, 0, 30, 30, 0, 30),
		ring(10, 10, 20, 10, 20, 20, 10, 20),
		ring(2, 2, 6, 2, 6, 6, 2, 6),
	}}
	fs, st := facadesOf(building, nil)
	var yard []Facade
	for _, f := range fs {
		if f.Courtyard {
			yard = append(yard, f)
		}
	}
	if want := []int{0, 90, 180, 270}; !equalInts(azimuths(yard), want) {
		t.Errorf("азимуты двора %v, want %v", azimuths(yard), want)
	}
	if len(fs) != 8 {
		t.Errorf("фасадов %d, want 8 (4 внешних + 4 дворовых, колодец не считается)", len(fs))
	}
	if st.Segments != 8 {
		t.Errorf("сегментов %d, want 8", st.Segments)
	}
	// Северная стена двора обращена на юг, в сторону двора.
	for _, f := range yard {
		mid := f.Probe().Mid
		x, y := testPlane.xy(mid)
		if f.AzimuthDeg == 180 && math.Abs(y-20) > 0.1 {
			t.Errorf("фасад на юг во дворе на y=%.1f (x=%.1f), want 20", y, x)
		}
	}
}

func TestShortWallsDropped(t *testing.T) {
	// Двор шириной 6 м: стены короче 8 м не считаются фасадами, снаружи 3 м и больше — считаются.
	narrow := orb.MultiPolygon{{
		ring(0, 0, 30, 0, 30, 30, 0, 30),
		ring(10, 10, 16, 10, 16, 20, 10, 20), // 6 × 10 м = 60 м²
	}}
	fs, _ := facadesOf(narrow, nil)
	for _, f := range fs {
		if f.Courtyard && f.LengthM < MinCourtyardFacadeM {
			t.Errorf("дворовый фасад %.1f м короче порога", f.LengthM)
		}
	}
	var yard int
	for _, f := range fs {
		if f.Courtyard {
			yard++
		}
	}
	if yard != 2 {
		t.Errorf("дворовых фасадов %d, want 2 (длинные стороны по 10 м)", yard)
	}
}

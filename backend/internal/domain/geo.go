// Package domain — сущности и правила без внешних зависимостей (кроме orb).
package domain

import "github.com/paulmach/orb"

// BBox — прямоугольник в градусах WGS84.
type BBox struct {
	South, West, North, East float64
}

// Contains сообщает, лежит ли точка (lon, lat) внутри прямоугольника.
func (b BBox) Contains(p orb.Point) bool {
	return p[1] >= b.South && p[1] <= b.North && p[0] >= b.West && p[0] <= b.East
}

// Pad расширяет прямоугольник на d градусов со всех сторон.
func (b BBox) Pad(d float64) BBox {
	return BBox{b.South - d, b.West - d, b.North + d, b.East + d}
}

// Union возвращает наименьший прямоугольник, содержащий оба.
func (b BBox) Union(o BBox) BBox {
	return BBox{min(b.South, o.South), min(b.West, o.West), max(b.North, o.North), max(b.East, o.East)}
}

// District — район охвата.
type District struct {
	Name string
	BBox BBox
}

// Districts — районы охвата. Береговые (vila_olimpica, diagonal_mar, barceloneta, poblenou)
// заданы огибающими реальных границ barrio из OSM (boundary=administrative, admin_level=10,
// выгрузка 24.10.2026, см. docs/data.md); gothic и eixample остались примерными контурами
// из PROJECT_CONTEXT.md, раздел 4.
// При пересечении побеждает район, стоящий раньше в списке: узкие береговые идут первыми.
var Districts = []District{
	{"vila_olimpica", BBox{41.3833, 2.1880, 41.3964, 2.2063}},
	{"diagonal_mar", BBox{41.3947, 2.2041, 41.4118, 2.2220}},
	{"barceloneta", BBox{41.3577, 2.1824, 41.3879, 2.1996}},
	{"poblenou", BBox{41.3915, 2.1939, 41.4075, 2.2108}},
	{"gothic", BBox{41.379, 2.170, 41.388, 2.182}},
	{"eixample", BBox{41.387, 2.160, 41.400, 2.178}},
}

// DistrictOf возвращает имя района точки или пустую строку, если точка вне охвата.
func DistrictOf(p orb.Point) string {
	for _, d := range Districts {
		if d.BBox.Contains(p) {
			return d.Name
		}
	}
	return ""
}

// CoverageBBox — прямоугольник, охватывающий все районы.
func CoverageBBox() BBox {
	b := Districts[0].BBox
	for _, d := range Districts[1:] {
		b = b.Union(d.BBox)
	}
	return b
}

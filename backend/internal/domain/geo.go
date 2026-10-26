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

// MetricSRID — метрическая проекция расчётов в метрах: ETRS89 / UTM 31N, родная для Барселоны.
// В этой проекции приходят контуры кадастра и карты шума, в ней PostGIS считает расстояния и лучи.
// Строка, а не число: константа подставляется в текст SQL. Проекция региона из regions — вместе со вторым регионом.
const MetricSRID = "25831"

// Region — регион охвата: контур и районы из таблиц regions и districts.
type Region struct {
	ID      int64
	Code    string
	Name    string
	Contour BBox
	// Districts идут по приоритету: при пересечении рамок побеждает район, стоящий раньше.
	Districts []District
}

// DistrictOf возвращает имя района точки или пустую строку, если точка вне районов региона.
func (r Region) DistrictOf(p orb.Point) string {
	for _, d := range r.Districts {
		if d.BBox.Contains(p) {
			return d.Name
		}
	}
	return ""
}

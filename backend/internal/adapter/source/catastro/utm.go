package catastro

import "math"

// toUTM31N переводит WGS84 (lon, lat в градусах) в ETRS89 / UTM 31N (EPSG:25831), метры.
// Ряды Крюгера: для сравнения рамок хватает с запасом (ошибка миллиметры).
func toUTM31N(lon, lat float64) (easting, northing float64) {
	const (
		a  = 6378137.0
		f  = 1 / 298.257222101
		k0 = 0.9996
	)
	n := f / (2 - f)
	e := math.Sqrt(f * (2 - f))
	bigA := a / (1 + n) * (1 + n*n/4 + n*n*n*n/64)
	alpha := [3]float64{
		n/2 - 2*n*n/3 + 5*n*n*n/16,
		13*n*n/48 - 3*n*n*n/5,
		61 * n * n * n / 240,
	}

	phi := lat * math.Pi / 180
	lam := (lon - 3) * math.Pi / 180 // центральный меридиан зоны 31 — 3° в.д.
	t := math.Sinh(math.Atanh(math.Sin(phi)) - e*math.Atanh(e*math.Sin(phi)))
	xi := math.Atan2(t, math.Cos(lam))
	eta := math.Atanh(math.Sin(lam) / math.Sqrt(1+t*t))
	xiSum, etaSum := xi, eta
	for j := 1; j <= 3; j++ {
		fj := float64(2 * j)
		xiSum += alpha[j-1] * math.Sin(fj*xi) * math.Cosh(fj*eta)
		etaSum += alpha[j-1] * math.Cos(fj*xi) * math.Sinh(fj*eta)
	}
	return 500000 + k0*bigA*etaSum, k0 * bigA * xiSum
}

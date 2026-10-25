package bcnnoise

import (
	"testing"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/wkb"
)

// gpkg оборачивает WKB в заголовок GeoPackage: «GP», версия 0, флаги, srs_id 25831 и рамка по флагам.
func gpkg(t *testing.T, g orb.Geometry, flags byte, envelope int) []byte {
	t.Helper()
	body, err := wkb.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	head := append([]byte{'G', 'P', 0, flags}, 0x27, 0x65, 0, 0) // 25831 = 0x6527, little endian
	head = append(head, make([]byte, envelope)...)
	return append(head, body...)
}

func TestParseGeometry(t *testing.T) {
	ml := orb.MultiLineString{{{430000, 4580000}, {430010, 4580000}}, {{430010, 4580000}, {430010, 4580008}}}
	for name, tc := range map[string]struct {
		flags    byte
		envelope int
	}{"без рамки": {0x01, 0}, "рамка XY": {0x03, 32}, "рамка XYZ": {0x05, 48}} {
		g, err := parseGeometry(gpkg(t, ml, tc.flags, tc.envelope))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := lines(g)
		if len(got) != 2 || got[1][1] != (orb.Point{430010, 4580008}) {
			t.Errorf("%s: %v", name, got)
		}
	}

	single, err := parseGeometry(gpkg(t, orb.LineString{{1, 2}, {3, 4}}, 0x01, 0))
	if err != nil || len(lines(single)) != 1 {
		t.Errorf("одиночная линия: %v, %v", single, err)
	}
	if _, err := parseGeometry([]byte("XX\x00\x01")); err == nil {
		t.Error("мусор разобран без ошибки")
	}
	if _, err := parseGeometry(gpkg(t, ml, 0x11, 0)); err == nil {
		t.Error("пустая геометрия разобрана без ошибки")
	}
}

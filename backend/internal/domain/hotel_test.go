package domain

import (
	"testing"

	"github.com/paulmach/orb"
)

func TestResolveHeight(t *testing.T) {
	five, h := 5, 21.5
	tests := []struct {
		name       string
		levels     *int
		height     *float64
		wantHeight float64
		wantSource string
	}{
		{"height wins", &five, &h, 21.5, HeightOSMHeight},
		{"only levels", &five, nil, 15, HeightOSMLevels},
		{"only height", nil, &h, 21.5, HeightOSMHeight},
		{"nothing", nil, nil, DefaultHeightM, HeightDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotH, gotSrc := ResolveHeight(tt.levels, tt.height)
			if gotH != tt.wantHeight || gotSrc != tt.wantSource {
				t.Errorf("got (%v, %s), want (%v, %s)", gotH, gotSrc, tt.wantHeight, tt.wantSource)
			}
		})
	}
}

func TestDistrictOf(t *testing.T) {
	tests := []struct {
		name string
		p    orb.Point
		want string
	}{
		{"barceloneta", orb.Point{2.19, 41.380}, "barceloneta"},
		{"gothic", orb.Point{2.176, 41.383}, "gothic"},
		{"eixample", orb.Point{2.165, 41.395}, "eixample"},
		{"overlap goes to gothic", orb.Point{2.172, 41.3875}, "gothic"},
		{"outside", orb.Point{2.10, 41.30}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DistrictOf(tt.p); got != tt.want {
				t.Errorf("DistrictOf(%v) = %q, want %q", tt.p, got, tt.want)
			}
		})
	}
}

func TestStoreID(t *testing.T) {
	if got := (Hotel{OSMType: "way", OSMID: 42}).StoreID(); got != -42 {
		t.Errorf("way: %d", got)
	}
	if got := (Hotel{OSMType: "node", OSMID: 42}).StoreID(); got != 42 {
		t.Errorf("node: %d", got)
	}
}

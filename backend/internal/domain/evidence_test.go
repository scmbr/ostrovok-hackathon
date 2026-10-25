package domain

import (
	"math"
	"testing"
)

func TestEvidenceContribution(t *testing.T) {
	cases := []struct {
		r, want float64
	}{
		{0.5, 0},
		{0.85, math.Log(0.85 / 0.15)},
		{0.99, math.Log(0.97 / 0.03)}, // ограничено сверху
		{0, math.Log(0.05 / 0.95)},    // ограничено снизу
	}
	for _, tc := range cases {
		got := Evidence{Reliability: tc.r}.Contribution()
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("r=%v: %v, want %v", tc.r, got, tc.want)
		}
	}
}

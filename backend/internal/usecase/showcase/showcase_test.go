package showcase

import (
	"context"
	"testing"
)

func ptrI(v int) *int         { return &v }
func ptrF(v float64) *float64 { return &v }

type fakeStore struct{ raw []Raw }

func (f fakeStore) DemoHotelsRaw(context.Context, int64) ([]Raw, error) { return f.raw, nil }

func TestBadges(t *testing.T) {
	cases := []struct {
		name  string
		raw   Raw
		sea   bool
		beach int
	}{
		{"нет фактов", Raw{}, false, 0},
		{"вид и пляж рядом", Raw{SeaMinFloor: ptrI(2), BeachMinutes: ptrF(4.4)}, true, 4},
		{"пляж ровно на пороге", Raw{BeachMinutes: ptrF(15)}, false, 15},
		{"пляж дальше порога", Raw{BeachMinutes: ptrF(15.5)}, false, 0},
		{"пляж под боком не округляется до нуля", Raw{BeachMinutes: ptrF(0.2)}, false, 1},
	}
	for _, c := range cases {
		sea, beach := Badges(c.raw)
		if sea != c.sea || beach != c.beach {
			t.Errorf("%s: получили (%v, %d), нужно (%v, %d)", c.name, sea, beach, c.sea, c.beach)
		}
	}
}

func TestListOrder(t *testing.T) {
	raw := []Raw{
		{ID: 1},
		{ID: 2, BeachMinutes: ptrF(9)},
		{ID: 3, SeaMinFloor: ptrI(1), BeachMinutes: ptrF(30)},
		{ID: 4, BeachMinutes: ptrF(3)},
	}
	got, err := (&Service{Store: fakeStore{raw}}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{3, 4, 2, 1}
	for i, h := range got {
		if h.ID != want[i] {
			t.Fatalf("порядок %v, нужен %v", ids(got), want)
		}
	}
}

func ids(hs []Hotel) []int64 {
	var out []int64
	for _, h := range hs {
		out = append(out, h.ID)
	}
	return out
}

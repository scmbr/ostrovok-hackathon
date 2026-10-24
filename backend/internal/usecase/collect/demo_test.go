package collect

import (
	"reflect"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func cand(id int64, district string, building, height bool) domain.DemoCandidate {
	return domain.DemoCandidate{ID: id, District: district, HasBuilding: building, KnownHeight: height}
}

func TestSelectDemoIDs(t *testing.T) {
	cands := []domain.DemoCandidate{
		cand(1, "barceloneta", false, false),
		cand(2, "barceloneta", true, true),
		cand(3, "gothic", false, false),
		cand(4, "gothic", true, true),
		cand(5, "gothic", true, false),
		cand(6, "eixample", true, true),
		cand(7, "eixample", false, false),
		cand(8, "eixample", true, false),
	}

	t.Run("все из Барселонеты, остальное по кругу с приоритетом качества", func(t *testing.T) {
		// 2 из Барселонеты + по одному лучшему из eixample (6) и gothic (4)
		got := SelectDemoIDs(cands, 4)
		if want := []int64{1, 2, 4, 6}; !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("второй круг берёт здание без высоты раньше отеля без здания", func(t *testing.T) {
		got := SelectDemoIDs(cands, 6)
		if want := []int64{1, 2, 4, 5, 6, 8}; !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("кандидатов меньше n", func(t *testing.T) {
		if got := SelectDemoIDs(cands, 100); len(got) != len(cands) {
			t.Errorf("got %d, want %d", len(got), len(cands))
		}
	})

	t.Run("n меньше Барселонеты", func(t *testing.T) {
		if got := SelectDemoIDs(cands, 1); !reflect.DeepEqual(got, []int64{2}) {
			t.Errorf("got %v, want [2]", got)
		}
	})
}

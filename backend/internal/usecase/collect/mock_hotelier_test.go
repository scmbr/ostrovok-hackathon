package collect

import (
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func TestMockPersonaDeterministic(t *testing.T) {
	if MockPersona(57, true) != domain.PersonaEmbellishing {
		t.Error("приукрашивающий определяется расчётом, а не хешем")
	}
	counts := map[string]int{}
	for id := int64(1); id <= 1000; id++ {
		p := MockPersona(id, false)
		if p != MockPersona(id, false) {
			t.Fatalf("персона отеля %d не детерминирована", id)
		}
		counts[p]++
	}
	if counts[domain.PersonaEmbellishing] != 0 {
		t.Error("без признака приукрашивания персоны embellishing быть не должно")
	}
	if l := counts[domain.PersonaLazy]; l < 250 || l > 350 {
		t.Errorf("ленивых ~30 %%: %d из 1000", l)
	}
}

// Отели из проверки на пяти демо-отелях: среди них нужны и честные, и ленивые.
func TestMockPersonaFiveDemoHotels(t *testing.T) {
	got := map[int64]string{}
	for _, id := range []int64{161, 188, 279, 343, 177} {
		got[id] = MockPersona(id, false)
	}
	lazy, honest := 0, 0
	for _, p := range got {
		switch p {
		case domain.PersonaLazy:
			lazy++
		case domain.PersonaHonest:
			honest++
		}
	}
	if lazy == 0 || honest == 0 {
		t.Fatalf("среди пяти отелей нужны честные и ленивые: %v", got)
	}
}

func TestMockTruthShares(t *testing.T) {
	pets, curtains, n := 0, 0, 2000
	for id := int64(1); id <= int64(n); id++ {
		rows := MockTruth(id)
		if len(rows) != 2 || rows[0].Key != PetsAllowedKey || rows[1].Key != BlackoutCurtainsKey {
			t.Fatalf("набор строк истины: %+v", rows)
		}
		if rows[0].Value["available"] == true {
			pets++
			if _, ok := rows[0].Value["max_kg"]; !ok {
				t.Fatal("у отеля, принимающего питомцев, есть детали")
			}
		}
		if rows[1].Value["available"] == true {
			curtains++
		}
	}
	if s := float64(pets) / float64(n); s < 0.26 || s > 0.34 {
		t.Errorf("питомцы ~30 %%: %.2f", s)
	}
	if s := float64(curtains) / float64(n); s < 0.56 || s > 0.64 {
		t.Errorf("шторы ~60 %%: %.2f", s)
	}
	a, b := MockTruth(42), MockTruth(42)
	if a[0].Value["available"] != b[0].Value["available"] || a[1].Value["available"] != b[1].Value["available"] {
		t.Error("истина не детерминирована")
	}
}

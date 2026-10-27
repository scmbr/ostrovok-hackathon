package domain

import (
	"strings"
	"testing"
)

func TestExplainWishStatuses(t *testing.T) {
	beach := Characteristic{Key: "walk_to_beach", Title: "Пляж пешком", ValueType: ValueNumber, ValueField: "minutes"}
	cases := []struct {
		name string
		e    WishEval
		want []string
	}{
		{"совпало", WishEval{Status: StatusMatch, Value: 7.0, Confidence: 0.7}, []string{"совпало (7 мин)", "уверенность 70 %"}},
		{"шанс с пожеланием", WishEval{Status: StatusChance, P: 0.25, Wish: &VariantWish{MinFloor: 4, Probability: 0.7}},
			[]string{"шанс 25 %", "этаж 4+", "шанс с ним 70 %"}},
		{"под вопросом", WishEval{Status: StatusUncertain, Caveat: "источники спорят", Confidence: 0.5}, []string{"под вопросом — источники спорят"}},
		{"нет данных", WishEval{Status: StatusUnknown}, []string{"данных нет, спросим отель"}},
	}
	for _, c := range cases {
		got := ExplainWish(beach, c.e)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q не содержит %q", c.name, got, w)
			}
		}
	}
}

func TestNormalizeUnmetIgnoresFormAndOrder(t *testing.T) {
	a, b := NormalizeUnmet("морской воздух"), NormalizeUnmet("И воздух морской!")
	if a == "" || a != b {
		t.Errorf("%q != %q", a, b)
	}
	if NormalizeUnmet("и в на") != "" {
		t.Error("одни служебные слова — пустой ключ")
	}
}

// Утверждение отельера с низкой репутацией помечается; нейтральная репутация и расчёт — нет.
func TestExplainWishLowTrustNote(t *testing.T) {
	pets := Characteristic{Key: "pets_allowed", Title: "Питомцы", ValueType: ValueBool}
	mk := func(trust float64, author string) WishEval {
		return WishEval{Status: StatusMatch, Confidence: 0.6, Sources: []EvidenceSource{{AuthorType: author, AuthorTrust: trust}}}
	}
	if got := ExplainWish(pets, mk(0.33, AuthorHotelier)); !strings.Contains(got, LowTrustNote) {
		t.Errorf("trust 0,33: пометки нет: %s", got)
	}
	for _, e := range []WishEval{mk(0.5, AuthorHotelier), mk(0.8, AuthorHotelier), mk(0.33, AuthorGuest), mk(0, AuthorTool)} {
		if got := ExplainWish(pets, e); strings.Contains(got, LowTrustNote) {
			t.Errorf("пометка не нужна: %s", got)
		}
	}
}

func TestTrustNoteOnlyForLowTrustHotelier(t *testing.T) {
	low := []EvidenceSource{{AuthorType: AuthorTool, AuthorTrust: 0}, {AuthorType: AuthorHotelier, AuthorTrust: 0.33}}
	if TrustNote(low) != LowTrustNote {
		t.Error("отельер с репутацией 0,33 должен давать пометку")
	}
	for _, src := range [][]EvidenceSource{
		{{AuthorType: AuthorHotelier, AuthorTrust: 0.5}},
		{{AuthorType: AuthorGuest, AuthorTrust: 0.2}},
		nil,
	} {
		if n := TrustNote(src); n != "" {
			t.Errorf("пометка %q без повода: %+v", n, src)
		}
	}
}

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

package domain_test

import (
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func TestGroupTitleWithRegistry(t *testing.T) {
	reg, _ := testLexicon(t)
	mk := func(id int64, score, minutes float64) domain.HotelResult {
		return domain.HotelResult{HotelID: id, Best: domain.Variant{Score: score, Evals: []domain.WishEval{
			{Key: "noise_night", Status: domain.StatusMatch},
			{Key: "walk_to_beach", Status: domain.StatusMismatch, Value: minutes},
		}}}
	}
	g := domain.Group{Kind: domain.GroupCompromise, FailedKey: "walk_to_beach", Hotels: []domain.HotelResult{mk(1, 0.7, 18), mk(2, 0.6, 7)}}
	if got, want := domain.GroupTitle(reg, g), "Тихо ночью, но до пляжа дальше (от 7 мин)"; got != want {
		t.Errorf("заголовок: %q, ждали %q", got, want)
	}

	// Без коротких форм (характеристики, добавленные текстом) — прежняя шаблонная форма.
	var plain []domain.Characteristic
	for _, c := range reg.All() {
		c.TitleMet, c.TitleUnmet = "", ""
		plain = append(plain, c)
	}
	preg, err := domain.NewRegistry(plain)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := domain.GroupTitle(preg, g), "Тишина ночью, но пешком до пляжа от 7 мин"; got != want {
		t.Errorf("шаблонный заголовок: %q, ждали %q", got, want)
	}

	// Нечисловое желание: человеческая форма без «— не совпало».
	g2 := domain.Group{Kind: domain.GroupCompromise, FailedKey: "noise_night", Hotels: []domain.HotelResult{{HotelID: 3, Best: domain.Variant{Evals: []domain.WishEval{
		{Key: "walk_to_beach", Status: domain.StatusMatch},
		{Key: "noise_night", Status: domain.StatusMismatch},
	}}}}}
	if got, want := domain.GroupTitle(reg, g2), "У пляжа, но ночью шумнее"; got != want {
		t.Errorf("заголовок: %q, ждали %q", got, want)
	}
	g.MustViolated = true
	if got := domain.GroupTitle(reg, g); got[len(got)-len("(не выполнено обязательное)"):] != "(не выполнено обязательное)" {
		t.Errorf("пометка must: %q", got)
	}
	wishes := []domain.Desire{{Key: "noise_night"}, {Key: "walk_to_beach"}}
	want := "Отелей, где есть всё сразу, не нашлось: тишина ночью и пешком до пляжа вместе не встречаются. Варианты:"
	if got := domain.GroupLead(reg, wishes, []domain.Group{g}); got != want {
		t.Errorf("строка над блоками: %q", got)
	}
}

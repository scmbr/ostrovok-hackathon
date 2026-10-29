package domain

import "testing"

func TestPriceInText(t *testing.T) {
	cases := []struct {
		text  string
		price int
		want  bool
	}{
		{"а можно подешевле?", 100, false},
		{"до 150 евро за ночь", 150, true},
		{"не дороже 1 500 €", 1500, true},
		{"2 звезды у моря", 150, false},
		{"бюджет 80€", 80, true},
		{"бюджет 80€", 0, false},
	}
	for _, c := range cases {
		if got := PriceInText(c.text, c.price); got != c.want {
			t.Errorf("PriceInText(%q, %d) = %v, ожидалось %v", c.text, c.price, got, c.want)
		}
	}
}

func TestGuardModelWish(t *testing.T) {
	must := Desire{Key: "sea_view", Importance: Must}
	cases := []struct {
		text    string
		current []Desire
		want    Importance
	}{
		{"тихий отель с видом", nil, Prefer},
		{"нужна аптека ночью", nil, Must},
		{"только с видом на море", nil, Must},
		{"must have sea view", nil, Must},
		{"обязательно с видом", nil, Must},
		// Обязательность из прошлых ходов не теряется.
		{"а теперь подешевле", []Desire{must}, Must},
	}
	for _, c := range cases {
		got, _ := GuardModelWish(c.text, must, c.current)
		if got.Importance != c.want {
			t.Errorf("GuardModelWish(%q) = %s, ожидалось %s", c.text, got.Importance, c.want)
		}
	}
	if got, changed := GuardModelWish("тихо", Desire{Key: "x", Importance: Prefer}, nil); changed || got.Importance != Prefer {
		t.Errorf("prefer не меняется: %+v %v", got, changed)
	}
}

package domain

import "testing"

func TestMakeControlKeepsMargin(t *testing.T) {
	cases := []struct {
		minutes int
		yes     bool
		want    int
		ok      bool
	}{
		{3, true, 10, true},   // метро в 3 минутах: «в пределах 10?» — да (запас больше двух)
		{3, false, 0, false},  // «нет» невозможно: порог 2 ближе чем на запас
		{12, false, 5, true},  // 12 минут до пляжа: «в пределах 5?» — нет
		{12, true, 30, true},  // порог 30 ≥ 24
		{9, true, 20, true},   // 10 < 18, берём 20
		{70, false, 30, true}, // наибольший порог не выше 35
		{0, true, 0, false},
	}
	for _, c := range cases {
		q, ok := MakeControl("walk_to_metro", c.minutes, c.yes)
		if ok != c.ok || (ok && q.Threshold != c.want) {
			t.Errorf("minutes=%d yes=%v: порог %d ok=%v, want %d ok=%v", c.minutes, c.yes, q.Threshold, ok, c.want, c.ok)
		}
		if ok {
			if c.yes && q.Threshold < q.Minutes*ControlMargin {
				t.Errorf("«да» без запаса: %+v", q)
			}
			if !c.yes && q.Threshold*ControlMargin > q.Minutes {
				t.Errorf("«нет» без запаса: %+v", q)
			}
		}
	}
}

func TestCareless(t *testing.T) {
	if got := Careless(60, []string{"yes", "no", "yes"}, true); len(got) != 0 {
		t.Errorf("добросовестная анкета: %v", got)
	}
	if got := Careless(3, []string{"yes", "no"}, true); len(got) != 1 || got[0] != CarelessFast {
		t.Errorf("быстрая: %v", got)
	}
	if got := Careless(60, []string{"yes", "yes", "yes"}, true); len(got) != 1 || got[0] != CarelessUniform {
		t.Errorf("одинаковые: %v", got)
	}
	if got := Careless(60, []string{"yes", "yes"}, true); len(got) != 0 {
		t.Errorf("два одинаковых ответа ещё не небрежность: %v", got)
	}
	if got := Careless(2, []string{"yes", "yes", "yes"}, false); len(got) != 3 {
		t.Errorf("все три причины: %v", got)
	}
}

func TestGuruPointsEqualForYesAndNo(t *testing.T) {
	if GuruPoints(3, nil) != 30 || GuruPoints(3, []string{CarelessFast}) != 0 {
		t.Error("баллы: 10 за ответ, у небрежной анкеты 0")
	}
}

// Сигнал считает только ответы против расчёта в своей ячейке, от надёжных гостей, по одному голосу с брони.
func TestSignalVotes(t *testing.T) {
	v := []SignalVote{
		{1, 1, 0.67, "none", "full"},
		{2, 2, 0.67, "none", "full"},
		{2, 2, 0.67, "none", "full"}, // дубль брони
		{3, 3, 0.33, "none", "full"}, // ленивый: репутация ниже порога
		{4, 4, 0.67, "none", "none"}, // согласен с расчётом
	}
	if got := len(SignalVotes(v)); got != 2 {
		t.Errorf("голосов %d, want 2", got)
	}
	if SignalReached(v) {
		t.Error("двух надёжных гостей мало")
	}
	v = append(v, SignalVote{5, 5, 0.75, "none", "full"})
	if !SignalReached(v) {
		t.Error("три надёжных гостя против расчёта — сигнал")
	}
}

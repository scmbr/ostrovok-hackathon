package domain

import (
	"strings"
	"testing"
)

func variant(name string, price int, evals ...WishEval) Variant {
	return NewVariant(1, name, 10, price, evals, map[string]bool{"sea_view": true})
}

func beachEval(minutes, score float64, imp Importance) WishEval {
	return WishEval{Key: "walk_to_beach", Importance: imp, Status: StatusMatch, Value: minutes, Confidence: 0.7, Score: score, P: 1}
}

func seaMatch() WishEval {
	return WishEval{Key: "sea_view", Importance: Prefer, Status: StatusMatch, Guarantee: true, Value: "full", Confidence: 0.85, Score: 0.95, P: 1}
}

func seaChance(p float64) WishEval {
	return WishEval{Key: "sea_view", Importance: Prefer, Status: StatusChance, Value: "none", Confidence: 0.85, P: p, Score: WishScore(p, 0.85),
		Wish: &VariantWish{MinFloor: 4, Probability: p}}
}

func TestCompareFewerViolationsWins(t *testing.T) {
	reg := searchRegistry(t)
	violated := WishEval{Key: "pets_allowed", Importance: Must, Status: StatusMismatch, Value: false, Confidence: 0.8, Score: 0.1}
	ok := WishEval{Key: "pets_allowed", Importance: Must, Status: StatusMatch, Value: true, Confidence: 0.8, Score: 0.9, P: 1}
	// у A оценка выше, но must нарушен
	a := variant("A", 100, violated, beachEval(2, 1, Prefer))
	b := variant("B", 120, ok, beachEval(15, 0.3, Prefer))
	wishes := []Desire{{Key: "pets_allowed", Importance: Must}, {Key: "walk_to_beach", Importance: Prefer}}
	c := Compare(reg, wishes, []CompareSide{{HotelID: 1, Label: "A", Variant: a}, {HotelID: 2, Label: "B", Variant: b}})
	if c.Winner != 1 || !strings.Contains(c.Verdict, "лучше B") || !strings.Contains(c.Verdict, "обязательное") {
		t.Fatalf("победитель %d, вывод %q", c.Winner, c.Verdict)
	}
	if len(c.Violations[0]) != 1 || len(c.Violations[1]) != 0 {
		t.Errorf("нарушения: %v", c.Violations)
	}
}

func TestCompareTieBelowThresholdAndSameWithinSignificantDiff(t *testing.T) {
	reg := searchRegistry(t)
	// 8 и 9 минут: ниже significant_diff = 2 — одинаково; оценки ближе 0,05 — равноценны.
	a := variant("A", 100, beachEval(8, 0.80, Prefer))
	b := variant("B", 90, beachEval(9, 0.78, Prefer))
	c := Compare(reg, []Desire{{Key: "walk_to_beach"}}, []CompareSide{{Label: "A", Variant: a}, {Label: "B", Variant: b}})
	if c.Winner != -1 || len(c.Same) != 1 || len(c.Differences) != 0 {
		t.Fatalf("победитель %d, различий %d, одинаково %d", c.Winner, len(c.Differences), len(c.Same))
	}
	if !strings.Contains(c.Verdict, "равноценны") || !strings.Contains(c.Verdict, "решает цена") {
		t.Errorf("вывод %q", c.Verdict)
	}
	// Граница порога: разница оценок 0,04 — равноценны, 0,06 — побеждает.
	hi := variant("A", 100, WishEval{Key: "pets_allowed", Importance: Prefer, Status: StatusMatch, Score: 0.80})
	for gap, wantTie := range map[float64]bool{0.04: true, 0.06: false} {
		lo := variant("B", 100, WishEval{Key: "pets_allowed", Importance: Prefer, Status: StatusMatch, Score: 0.80 - gap})
		c := Compare(reg, nil, []CompareSide{{Label: "A", Variant: hi}, {Label: "B", Variant: lo}})
		if (c.Winner == -1) != wantTie {
			t.Errorf("разрыв %v: победитель %d, ждали ничью %v", gap, c.Winner, wantTie)
		}
	}
}

func TestCompareTradeoffAndDayGroups(t *testing.T) {
	reg := searchRegistry(t)
	// Вид гарантирован в Vista mar за +30 €, в Standard — шанс ~25 % с пожеланием.
	std := variant("Standard", 100, seaChance(0.25), beachEval(5, 0.95, Prefer))
	vista := variant("Vista mar", 130, seaMatch(), beachEval(5, 0.95, Prefer))
	wishes := []Desire{{Key: "sea_view"}, {Key: "walk_to_beach"}}
	c := Compare(reg, wishes, []CompareSide{{Label: "Standard", Variant: std}, {Label: "Vista mar", Variant: vista}})
	if c.Winner != 1 {
		t.Fatalf("победитель %d: %s", c.Winner, c.Verdict)
	}
	if !strings.Contains(c.Tradeoff, "+30 €") || !strings.Contains(c.Tradeoff, "гарантирован") || !strings.Contains(c.Tradeoff, "шанс ~25 %") {
		t.Errorf("цена компромисса: %q", c.Tradeoff)
	}
	// У характеристик в тестовом реестре нет day_part — всё в «Прочее».
	if len(c.Days) != 1 || c.Days[0].Part != DayOther || len(c.Days[0].Keys) != 2 {
		t.Errorf("группы дня: %+v", c.Days)
	}
	// Лучший вариант и дешевле остальных.
	cheap := variant("Дешёвый", 90, seaMatch())
	c = Compare(reg, []Desire{{Key: "sea_view"}}, []CompareSide{{Label: "Дорогой", Variant: std}, {Label: "Дешёвый", Variant: cheap}})
	if c.Winner != 1 || !strings.Contains(c.Tradeoff, "дешевле") {
		t.Errorf("победитель %d, цена компромисса %q", c.Winner, c.Tradeoff)
	}
}

func TestCompareDayPartsFromRegistry(t *testing.T) {
	reg, err := NewRegistry([]Characteristic{
		{Key: "noise_night", Level: LevelHotel, ValueType: ValueBool, ValueField: "v", DayPart: DayNight},
		{Key: "walk_to_beach", Level: LevelHotel, ValueType: ValueBool, ValueField: "v", DayPart: DayDay},
		{Key: "pets_allowed", Level: LevelHotel, ValueType: ValueBool, ValueField: "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := func(k string, s float64) WishEval {
		return WishEval{Key: k, Importance: Prefer, Status: StatusMatch, Score: s}
	}
	a := variant("A", 100, ev("noise_night", 1), ev("walk_to_beach", 1), ev("pets_allowed", 1))
	b := variant("B", 100, ev("noise_night", 0.5), ev("walk_to_beach", 0.5), ev("pets_allowed", 0.5))
	ws := []Desire{{Key: "pets_allowed"}, {Key: "walk_to_beach"}, {Key: "noise_night"}}
	c := Compare(reg, ws, []CompareSide{{Label: "A", Variant: a}, {Label: "B", Variant: b}})
	var parts []string
	for _, g := range c.Days {
		parts = append(parts, g.Part)
	}
	if strings.Join(parts, ",") != "day,night,other" {
		t.Errorf("части дня: %v", parts)
	}
}

func TestCompareUncertainHasAskAction(t *testing.T) {
	reg := searchRegistry(t)
	unk := WishEval{Key: "pets_allowed", Importance: Prefer, Status: StatusUnknown, Score: UnknownScore, Caveat: "данных нет — спросим отель"}
	c := Compare(reg, []Desire{{Key: "pets_allowed"}}, []CompareSide{
		{HotelID: 7, Label: "A", Variant: variant("A", 100, unk)}, {HotelID: 8, Label: "B", Variant: variant("B", 100, seaMatch())}})
	if len(c.Uncertain) != 1 || c.Uncertain[0].HotelID != 7 || c.Uncertain[0].Key != "pets_allowed" {
		t.Errorf("неуверенное: %+v", c.Uncertain)
	}
}

func TestContractLines(t *testing.T) {
	reg := searchRegistry(t)
	conf := seaChance(0.5)
	conf.Wish.HotelConfirmed = true
	refused := seaChance(0.3)
	refused.Wish.HotelRefused = true
	cases := []struct {
		name     string
		e        WishEval
		withWish bool
		kind     string
		ask      bool
	}{
		{"гарантия", seaMatch(), false, ContractGuaranteed, false},
		{"шанс без пожелания", seaChance(0.25), false, ContractChance, false},
		{"пожелание, отель молчит", seaChance(0.25), true, ContractWishPending, false},
		{"отель подтвердил", conf, true, ContractWishConfirmed, false},
		{"отель не гарантирует", refused, true, ContractWishRefused, false},
		{"под вопросом", WishEval{Key: "sea_view", Status: StatusUncertain, Conflict: "спор"}, false, ContractUncertain, true},
		{"неизвестно", WishEval{Key: "sea_view", Status: StatusUnknown}, false, ContractUnknown, true},
	}
	for _, tc := range cases {
		l := ContractLines(reg, variant("X", 100, tc.e), tc.withWish)
		if len(l) != 1 || l[0].Kind != tc.kind || l[0].CanAsk != tc.ask {
			t.Errorf("%s: %+v", tc.name, l)
		}
	}
}

func TestParseCompareCommand(t *testing.T) {
	cases := []struct {
		in        string
		ok        bool
		shortlist bool
		refs      []SimilarRef
	}{
		{"сравни первый и третий", true, false, []SimilarRef{{Ordinal: 1}, {Ordinal: 3}}},
		{"Сравни 2 и 3, пожалуйста", true, false, []SimilarRef{{Ordinal: 2}, {Ordinal: 3}}},
		{"сравни Hotel Arts и Hotel Omm", true, false, []SimilarRef{{Name: "hotel arts"}, {Name: "hotel omm"}}},
		{"сравни шортлист", true, true, nil},
		{"сравните отель Arts с отелем Omm", true, false, []SimilarRef{{Name: "arts"}, {Name: "omm"}}},
		{"хочу тихий отель", false, false, nil},
	}
	for _, c := range cases {
		cmd, ok := ParseCompareCommand(c.in)
		if ok != c.ok || cmd.Shortlist != c.shortlist || len(cmd.Refs) != len(c.refs) {
			t.Errorf("%q: %+v ok=%v", c.in, cmd, ok)
			continue
		}
		for i := range c.refs {
			if cmd.Refs[i] != c.refs[i] {
				t.Errorf("%q: ссылка %d = %+v, ждали %+v", c.in, i, cmd.Refs[i], c.refs[i])
			}
		}
	}
}

// Шанс из пожелания к брони называется честно: «~70 % с пожеланием», а не просто «шанс».
func TestShortStatusNamesWish(t *testing.T) {
	reg := searchRegistry(t)
	c, _ := reg.Get("sea_view")
	chance := WishEval{Key: "sea_view", Status: StatusChance, P: 0.56, PBase: 0.25, PWish: 0.56, Wish: &VariantWish{Probability: 0.56}}
	if got := shortStatus(c, chance); got != "шанс ~56 % с пожеланием" {
		t.Errorf("шанс: %q", got)
	}
	doubt := WishEval{Key: "sea_view", Status: StatusUncertain, P: 0.7, PBase: 0.39, PWish: 0.7, Wish: &VariantWish{Probability: 0.7}}
	if got := shortStatus(c, doubt); got != "под вопросом, ~70 % с пожеланием" {
		t.Errorf("под вопросом: %q", got)
	}
}

// Несовпадение с открытым конфликтом («Sea View» против расчёта) в контракте и сравнении — «под вопросом».
func TestMismatchWithConflictIsUncertain(t *testing.T) {
	reg := searchRegistry(t)
	e := WishEval{Key: "sea_view", Importance: Must, Status: StatusMismatch, Confidence: 0.79, Conflict: "view=full: hotelier 0.41; view=none: computed 1.73"}
	l := ContractLines(reg, variant("Sea View", 100, e), false)[0]
	if l.Kind != ContractUncertain || !l.CanAsk || l.Conflict == "" || !strings.Contains(l.Text, "источники спорят") {
		t.Errorf("контракт: %+v", l)
	}
	c, _ := reg.Get("sea_view")
	if got := shortStatus(c, e); got != "под вопросом (источники спорят)" {
		t.Errorf("сравнение: %q", got)
	}
	plain := e
	plain.Conflict = ""
	if l := ContractLines(reg, variant("X", 100, plain), false)[0]; l.Kind != ContractMismatch {
		t.Errorf("без конфликта остаётся «не совпадёт»: %+v", l)
	}
}

// Расчёт неуверенный (уверенность 50 %), но гость бронирует с пожеланием: видны шанс с пожеланием и ответ отеля.
func TestContractUncertainWithWish(t *testing.T) {
	reg := searchRegistry(t)
	e := WishEval{Key: "sea_view", Importance: Prefer, Status: StatusUncertain, Confidence: 0.5, P: 0.7, PBase: 0.39, PWish: 0.7,
		Wish: &VariantWish{Probability: 0.7, HotelConfirmed: true}}
	l := ContractLines(reg, variant("Standard", 100, e), true)[0]
	if l.Kind != ContractWishConfirmed || l.Probability != 0.7 || !l.CanAsk || !strings.Contains(l.Text, "уверенность в расчёте 50 %") {
		t.Errorf("%+v", l)
	}
	if l := ContractLines(reg, variant("Standard", 100, e), false)[0]; l.Kind != ContractUncertain {
		t.Errorf("без пожелания остаётся «под вопросом»: %+v", l)
	}
}

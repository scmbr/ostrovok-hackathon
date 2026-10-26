package domain

import (
	"math"
	"testing"
)

func searchRegistry(t *testing.T) Registry {
	t.Helper()
	reg, err := NewRegistry([]Characteristic{
		{Key: "walk_to_beach", Level: LevelHotel, ValueType: ValueNumber, ValueField: "minutes", SignificantDiff: 2},
		{Key: "night_services", Level: LevelHotel, ValueType: ValueEnum, Values: []string{"none", "basic", "rich"}, Ordered: true,
			ValueField: "level", FavorableValues: []string{"basic", "rich"}},
		{Key: "pets_allowed", Level: LevelHotel, ValueType: ValueBool, ValueField: "available"},
		{Key: "sea_view", Level: LevelFacade, ValueType: ValueRange, ValueField: "min_floor",
			Category: &CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full", FloorField: "min_floor"}},
		{Key: "climate_by_month", Level: LevelDistrict, ValueType: ValueRange},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestNormalizeWishFromRegistry(t *testing.T) {
	reg := searchRegistry(t)
	cases := []struct {
		name    string
		in      Desire
		wantErr bool
		check   func(Desire) bool
	}{
		{"число: le по умолчанию", Desire{Key: "walk_to_beach", Value: 10}, false, func(w Desire) bool { return w.Op == OpLe && w.Value == 10.0 && w.Importance == Prefer }},
		{"упорядоченный enum: значение или лучше", Desire{Key: "night_services", Value: "basic"}, false, func(w Desire) bool { return w.Op == OpGe }},
		{"bool: true по умолчанию", Desire{Key: "pets_allowed", Importance: Must}, false, func(w Desire) bool { return w.Value == true && w.Op == OpEq }},
		{"категория: цель из реестра", Desire{Key: "sea_view", MinFloor: 3}, false, func(w Desire) bool { return w.Value == "full" }},
		{"нет в реестре", Desire{Key: "ghost"}, true, nil},
		{"район не ищется", Desire{Key: "climate_by_month"}, true, nil},
		{"значение не из шкалы", Desire{Key: "night_services", Value: "huge"}, true, nil},
		{"этаж у характеристики отеля", Desire{Key: "pets_allowed", MinFloor: 2}, true, nil},
		{"важность", Desire{Key: "pets_allowed", Importance: "nice"}, true, nil},
		{"число без числа", Desire{Key: "walk_to_beach", Value: "много"}, true, nil},
	}
	for _, c := range cases {
		w, _, err := NormalizeWish(reg, c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: ошибка %v", c.name, err)
		}
		if err == nil && !c.check(w) {
			t.Errorf("%s: получили %+v", c.name, w)
		}
	}
}

// Число у порога: в пределах significant_diff — «почти», p = 0,5, нарушением must не считается.
func TestEvalHotelWishNearThreshold(t *testing.T) {
	reg := searchRegistry(t)
	c, _ := reg.Get("walk_to_beach")
	w, _, _ := NormalizeWish(reg, Desire{Key: "walk_to_beach", Value: 10.0, Importance: Must})
	cases := []struct {
		minutes float64
		status  Status
		violate bool
		caveat  string
	}{
		{9.5, StatusNear, false, "≈ 9,5 мин, на границе"},
		{10.5, StatusNear, false, "≈ 10,5 мин, на границе"},
		{14, StatusMismatch, true, ""},
		{6, StatusMatch, false, ""},
	}
	for _, tc := range cases {
		e := EvalHotelWish(c, w, &Fact{Value: map[string]any{"minutes": tc.minutes}, Confidence: 0.7})
		if e.Status != tc.status || e.Violated() != tc.violate || e.Caveat != tc.caveat {
			t.Errorf("%.1f мин: статус %s, нарушение %v, оговорка %q", tc.minutes, e.Status, e.Violated(), e.Caveat)
		}
		if tc.status == StatusNear && e.P != NearProbability {
			t.Errorf("%.1f мин: p = %v, ждали 0,5", tc.minutes, e.P)
		}
	}
}

func TestEvalHotelWishUnknownIsNotNo(t *testing.T) {
	reg := searchRegistry(t)
	c, _ := reg.Get("pets_allowed")
	w, _, _ := NormalizeWish(reg, Desire{Key: "pets_allowed", Importance: Must})
	e := EvalHotelWish(c, w, nil)
	if e.Status != StatusUnknown || e.Violated() || e.Score != UnknownScore {
		t.Errorf("нет данных: %+v", e)
	}
	no := EvalHotelWish(c, w, &Fact{Value: map[string]any{"available": false}, Confidence: 0.8})
	if no.Status != StatusMismatch || !no.Violated() || no.Score >= e.Score {
		t.Errorf("«нет» с уверенностью 0,8 должно быть хуже неизвестного: %+v", no)
	}
}

func TestEvalHotelWishOrderedEnum(t *testing.T) {
	reg := searchRegistry(t)
	c, _ := reg.Get("night_services")
	w, _, _ := NormalizeWish(reg, Desire{Key: "night_services", Value: "basic"})
	for level, want := range map[string]Status{"rich": StatusMatch, "basic": StatusMatch, "none": StatusMismatch} {
		if e := EvalHotelWish(c, w, &Fact{Value: map[string]any{"level": level}, Confidence: 0.8}); e.Status != want {
			t.Errorf("%s: статус %s, ждали %s", level, e.Status, want)
		}
	}
	e := EvalHotelWish(c, w, &Fact{Value: map[string]any{"level": "rich"}, Confidence: 0.5})
	if e.Status != StatusUncertain {
		t.Errorf("уверенность 0,5: ждали «под вопросом», получили %s", e.Status)
	}
}

// seaViewInput — категория из 10 номеров на одном фасаде, этажи 1–10; море видно с 8-го этажа (3 номера из 10).
func seaViewInput(fact *Fact, guaranteed bool) CategoryWishInput {
	at := func(_ int64, floor int) (string, bool) {
		if floor >= 8 {
			return "full", true
		}
		return "none", true
	}
	a := AssessCategory(10, []Placement{{FacadeID: 1, FloorFrom: 1, FloorTo: 10, Rooms: 10}}, at, "full")
	return CategoryWishInput{Assessment: a, Fact: fact, WishGuaranteed: guaranteed, AzimuthOf: func(int64) float64 { return 90 }}
}

func seaViewWish(t *testing.T, imp Importance) (Characteristic, Desire) {
	reg := searchRegistry(t)
	w, c, err := NormalizeWish(reg, Desire{Key: "sea_view", Importance: imp})
	if err != nil {
		t.Fatal(err)
	}
	return c, w
}

// Отель с 30 % в единственной категории и пожеланием не попадает в нарушения; без пожелания — попадает.
func TestEvalCategoryWishWishAvoidsViolation(t *testing.T) {
	c, w := seaViewWish(t, Must)
	fact := &Fact{Value: map[string]any{"view": "none"}, Confidence: 0.85}
	e := EvalCategoryWish(c, w, seaViewInput(fact, false))
	if math.Abs(e.PBase-0.3) > 1e-9 {
		t.Fatalf("p_base = %v, ждали 0,3", e.PBase)
	}
	if e.Wish == nil || math.Abs(e.PWish-WishFulfilProbability) > 1e-9 {
		t.Fatalf("p_wish = %v, ждали %v, пожелание %+v", e.PWish, WishFulfilProbability, e.Wish)
	}
	if e.P != e.PWish || e.Status != StatusChance || e.Violated() {
		t.Errorf("с пожеланием: p %v, статус %s, нарушение %v", e.P, e.Status, e.Violated())
	}
	if e.Wish.MinFloor != 8 || e.Wish.HotelConfirmed {
		t.Errorf("пожелание: %+v", e.Wish)
	}
	// Тот же случай без пожелания (набора нет): остаётся шанс 30 %, must нарушен.
	in := seaViewInput(fact, false)
	in.Assessment.Wish = nil
	if no := EvalCategoryWish(c, w, in); !no.Violated() || no.P != 0.3 {
		t.Errorf("без пожелания: p %v, нарушение %v", no.P, no.Violated())
	}
}

func TestEvalCategoryWishHotelGuaranteesWishes(t *testing.T) {
	c, w := seaViewWish(t, Prefer)
	fact := &Fact{Value: map[string]any{"view": "none"}, Confidence: 0.85}
	plain := EvalCategoryWish(c, w, seaViewInput(fact, false))
	conf := EvalCategoryWish(c, w, seaViewInput(fact, true))
	if conf.PWish != WishGuaranteedProbability || !conf.Wish.HotelConfirmed || conf.Score <= plain.Score {
		t.Errorf("подтверждение отеля: p_wish %v, оценка %v против %v", conf.PWish, conf.Score, plain.Score)
	}
}

func guaranteedInput(fact *Fact) CategoryWishInput {
	at := func(int64, int) (string, bool) { return "full", true }
	a := AssessCategory(10, []Placement{{FacadeID: 1, FloorFrom: 1, FloorTo: 5, Rooms: 10}}, at, "full")
	return CategoryWishInput{Assessment: a, Fact: fact}
}

func TestEvalCategoryWishGuarantee(t *testing.T) {
	c, w := seaViewWish(t, Must)
	e := EvalCategoryWish(c, w, guaranteedInput(&Fact{Value: map[string]any{"view": "full"}, Confidence: 0.9}))
	if e.Status != StatusMatch || !e.Guarantee || e.P != 1 || e.Wish != nil {
		t.Errorf("гарантия: %+v", e)
	}
}

// Название «Sea View» не даёт гарантии: расчёт фасадов без вида, факт категории none, конфликт открыт.
func TestEvalCategoryWishTitleDoesNotGuarantee(t *testing.T) {
	c, w := seaViewWish(t, Must)
	at := func(int64, int) (string, bool) { return "none", true }
	a := AssessCategory(10, []Placement{{FacadeID: 1, FloorFrom: 1, FloorTo: 5, Rooms: 10}}, at, "full")
	fact := &Fact{Value: map[string]any{"view": "none"}, Confidence: 0.79, Conflict: &Conflict{Description: "название обещает вид"}}
	e := EvalCategoryWish(c, w, CategoryWishInput{Assessment: a, Fact: fact})
	if e.Guarantee || e.Status != StatusMismatch || !e.Violated() || e.Conflict == "" || e.P != 0 {
		t.Errorf("приукрашенная категория: %+v", e)
	}
}

// Гости против расчёта (отель 156): расчёт гарантирует вид, итог движка — none; гарантии нет, конфликт виден.
func TestEvalCategoryWishEngineOverridesCalculation(t *testing.T) {
	c, w := seaViewWish(t, Must)
	fact := &Fact{Value: map[string]any{"view": "none"}, Confidence: 0.84, Conflict: &Conflict{Description: "гости против расчёта"}}
	e := EvalCategoryWish(c, w, guaranteedInput(fact))
	if e.Guarantee || e.Status != StatusUncertain || e.Conflict == "" || e.P > 0.2 {
		t.Errorf("конфликт с гостями: %+v", e)
	}
}

func TestChooseVariants(t *testing.T) {
	keys := map[string]bool{"sea_view": true}
	guar := WishEval{Key: "sea_view", Importance: Must, Status: StatusMatch, Guarantee: true, Score: 0.9}
	chance := WishEval{Key: "sea_view", Importance: Must, Status: StatusChance, Score: 0.6}
	viol := WishEval{Key: "sea_view", Importance: Must, Status: StatusMismatch, Score: 0.1}
	deluxe := NewVariant(1, "Deluxe", 10, 180, []WishEval{guar}, keys)
	standard := NewVariant(2, "Standard", 20, 150, []WishEval{chance}, keys)
	bad := NewVariant(3, "Family", 5, 100, []WishEval{viol}, keys)
	best, alt, ok := ChooseVariants([]Variant{standard, bad, deluxe})
	if !ok || best.Name != "Deluxe" || !best.Guaranteed {
		t.Fatalf("лучший: %+v", best)
	}
	if alt == nil || alt.Name != "Standard" {
		t.Fatalf("альтернатива дешевле: %+v", alt)
	}
	// Нарушающий вариант дешевле, но альтернативой не становится.
	if alt.Name == "Family" {
		t.Error("альтернатива с нарушением")
	}
	if _, alt, _ := ChooseVariants([]Variant{deluxe}); alt != nil {
		t.Error("одна категория — альтернативы нет")
	}
	// Лучший дешевле и без гарантии, альтернатива дороже, но с гарантией.
	cheap := NewVariant(4, "Standard", 20, 100, []WishEval{{Key: "sea_view", Importance: Prefer, Status: StatusChance, Score: 0.95}}, keys)
	costly := NewVariant(5, "Deluxe", 10, 130, []WishEval{{Key: "sea_view", Importance: Prefer, Status: StatusMatch, Guarantee: true, Score: 0.9}}, keys)
	best, alt, _ = ChooseVariants([]Variant{cheap, costly})
	if best.Name != "Standard" || alt == nil || alt.Name != "Deluxe" {
		t.Errorf("гарантия как альтернатива: best %s, alt %+v", best.Name, alt)
	}
}

func TestRankHotelsViolatorsLast(t *testing.T) {
	hs := []HotelResult{
		{HotelID: 1, Best: Variant{Score: 0.95, Violations: []string{"sea_view"}}},
		{HotelID: 2, Best: Variant{Score: 0.6}},
		{HotelID: 3, Best: Variant{Score: 0.8, PriceEUR: 120}},
		{HotelID: 4, Best: Variant{Score: 0.8, PriceEUR: 100}},
	}
	RankHotels(hs)
	got := []int64{hs[0].HotelID, hs[1].HotelID, hs[2].HotelID, hs[3].HotelID}
	want := []int64{4, 3, 2, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ждали %v", got, want)
		}
	}
}

func TestCategoryPrice(t *testing.T) {
	card := Card{PriceEUR: 100}
	for name, want := range map[string]int{"Standard": 100, "Deluxe": 145, "Sea View": 130, "Vista mar": 130, "Unknown": 100} {
		if got := CategoryPriceEUR(card, name); got != want {
			t.Errorf("%s: %d, ждали %d", name, got, want)
		}
	}
}

func TestProfileDistance(t *testing.T) {
	reg := searchRegistry(t)
	mk := func(minutes float64, level string, pets *bool) Profile {
		fs := []Fact{
			{EntityType: EntityHotel, Key: "walk_to_beach", Value: map[string]any{"minutes": minutes}},
			{EntityType: EntityHotel, Key: "night_services", Value: map[string]any{"level": level}},
		}
		if pets != nil {
			fs = append(fs, Fact{EntityType: EntityHotel, Key: "pets_allowed", Value: map[string]any{"available": *pets}})
		}
		return BuildProfile(reg, fs)
	}
	a, b, far := mk(10, "rich", nil), mk(11, "rich", nil), mk(30, "none", nil)
	if d, _ := ProfileDistance(reg, a, b, nil); d != 0 {
		t.Errorf("разница ниже significant_diff должна давать 0, получили %v", d)
	}
	if d, _ := ProfileDistance(reg, a, far, nil); d != 1 {
		t.Errorf("совсем разные: %v", d)
	}
	yes := true
	if d, dims := ProfileDistance(reg, a, mk(10, "rich", &yes), nil); d <= 0 || !dims[len(dims)-1].Unknown {
		t.Errorf("неизвестное у одного отеля даёт расстояние: %v %+v", d, dims)
	}
	// Вес из запроса: пляж важнее — удалённость по нему сильнее влияет.
	other := mk(10, "none", nil)
	plain, _ := ProfileDistance(reg, a, other, nil)
	weighted, _ := ProfileDistance(reg, a, other, map[string]float64{"walk_to_beach": 3})
	if weighted >= plain {
		t.Errorf("с большим весом совпавшей характеристики расстояние должно падать: %v >= %v", weighted, plain)
	}
}

func TestQueryWeights(t *testing.T) {
	w := QueryWeights([]Desire{{Key: "a", Importance: Must}, {Key: "b", Importance: Prefer}})
	if w["a"] != ProfileWeightMust || w["b"] != ProfileWeightPrefer {
		t.Errorf("%v", w)
	}
}

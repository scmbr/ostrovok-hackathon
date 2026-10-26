package domain

import (
	"math"
	"testing"
)

// viewFrom строит ValueAt для вида на море: у фасада известен этаж, с которого видно море (0 — вида нет).
func viewFrom(minFloor map[int64]int) ValueAt {
	return func(facade int64, floor int) (string, bool) {
		m, ok := minFloor[facade]
		if !ok {
			return "", false
		}
		if m > 0 && floor >= m {
			return "full", true
		}
		return "none", true
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Пример из контекста: в «Стандарте» 20 номеров, из них 6 на восточном фасаде, этажи 2–6. Номера делятся по
// этажам поровну (1,2 на этаж): при виде с 4-го этажа под ним 3 этажа из 5 — 3,6 номера, 18 %;
// при виде с 3-го — 4 этажа из 5, 4,8 номера, 24 % (то самое «~25 %» из контекста).
func TestAssessCategoryContextExample(t *testing.T) {
	ps := []Placement{{FacadeID: 1, FloorFrom: 2, FloorTo: 6, Rooms: 6}, {FacadeID: 2, FloorFrom: 1, FloorTo: 4, Rooms: 14}}
	for _, tt := range []struct {
		viewFrom int
		want     float64
	}{{4, 0.18}, {3, 0.24}} {
		a := AssessCategory(20, ps, viewFrom(map[int64]int{1: tt.viewFrom, 2: 0}), "full")
		if !approx(a.Probability, tt.want) {
			t.Errorf("вид с %d-го этажа: вероятность %.4f, ждали %.2f", tt.viewFrom, a.Probability, tt.want)
		}
		if a.Guarantee {
			t.Errorf("вид с %d-го этажа: гарантии быть не может, на втором фасаде вида нет", tt.viewFrom)
		}
		if !approx(a.Shares["full"]+a.Shares["none"], 1) || a.Unknown != 0 {
			t.Errorf("доли %v, неизвестно %v: должны покрывать все номера", a.Shares, a.Unknown)
		}
	}
}

func TestAssessCategoryWish(t *testing.T) {
	ps := []Placement{{FacadeID: 1, FloorFrom: 2, FloorTo: 6, Rooms: 10}, {FacadeID: 2, FloorFrom: 1, FloorTo: 4, Rooms: 10}}
	// Восток видит море с 4-го этажа (3 этажа из 5 → 6 номеров), север с 3-го (2 этажа из 4 → 5 номеров).
	a := AssessCategory(20, ps, viewFrom(map[int64]int{1: 4, 2: 3}), "full")
	if a.Wish == nil || a.Wish.FacadeID != 1 || a.Wish.MinFloor != 4 || !approx(a.Wish.Rooms, 6) || !approx(a.Wish.Share, 0.3) {
		t.Errorf("лучшее пожелание: %+v, ждали фасад 1, этаж 4+, 6 номеров (30 %%)", a.Wish)
	}
	if !approx(a.Probability, 0.55) {
		t.Errorf("вероятность %.4f, ждали 0,55 (11 номеров из 20)", a.Probability)
	}
}

func TestAssessCategoryWishWithoutFloorLimit(t *testing.T) {
	// На фасаде 1 вид на всех этажах категории, на фасаде 2 вида нет: пожелание «фасад 1» без этажа.
	ps := []Placement{{FacadeID: 1, FloorFrom: 3, FloorTo: 5, Rooms: 6}, {FacadeID: 2, FloorFrom: 1, FloorTo: 2, Rooms: 6}}
	a := AssessCategory(12, ps, viewFrom(map[int64]int{1: 2, 2: 0}), "full")
	if a.Wish == nil || a.Wish.FacadeID != 1 || a.Wish.MinFloor != 0 || !approx(a.Wish.Share, 0.5) {
		t.Errorf("пожелание: %+v, ждали фасад 1 без ограничения по этажу, 50 %%", a.Wish)
	}
}

func TestAssessCategoryGuarantee(t *testing.T) {
	ps := []Placement{{FacadeID: 1, FloorFrom: 4, FloorTo: 7, Rooms: 8}, {FacadeID: 3, FloorFrom: 5, FloorTo: 6, Rooms: 4}}
	a := AssessCategory(12, ps, viewFrom(map[int64]int{1: 4, 3: 2}), "full")
	if !a.Guarantee || !approx(a.Probability, 1) || a.Wish != nil {
		t.Errorf("гарантия: %+v — все номера выше этажа начала вида", a)
	}
	// Один этаж ниже порога снимает гарантию.
	ps[0].FloorFrom = 3
	a = AssessCategory(12, ps, viewFrom(map[int64]int{1: 4, 3: 2}), "full")
	if a.Guarantee || a.Probability >= 1 {
		t.Errorf("гарантия при номерах без вида: %+v", a)
	}
}

func TestAssessCategoryNoView(t *testing.T) {
	// «Sea View» на фасадах без вида: вероятность 0, пожелания нет.
	ps := []Placement{{FacadeID: 5, FloorFrom: 1, FloorTo: 5, Rooms: 10}}
	a := AssessCategory(10, ps, viewFrom(map[int64]int{5: 0}), "full")
	if a.Probability != 0 || a.Guarantee || a.Wish != nil || !approx(a.Shares["none"], 1) {
		t.Errorf("категория без вида: %+v", a)
	}
}

func TestAssessCategoryUnknown(t *testing.T) {
	// Фасад 9 без факта и 4 номера не размещены: гарантии нет, неизвестная доля видна.
	ps := []Placement{{FacadeID: 1, FloorFrom: 1, FloorTo: 2, Rooms: 4}, {FacadeID: 9, FloorFrom: 1, FloorTo: 2, Rooms: 2}}
	a := AssessCategory(10, ps, viewFrom(map[int64]int{1: 1}), "full")
	if a.Guarantee || !approx(a.Probability, 0.4) || !approx(a.Unknown, 0.6) {
		t.Errorf("неизвестные номера: вероятность %.2f, неизвестно %.2f, гарантия %v", a.Probability, a.Unknown, a.Guarantee)
	}
	if got := AssessCategory(0, nil, viewFrom(nil), "full"); got.Guarantee || got.Probability != 0 {
		t.Errorf("пустая категория: %+v", got)
	}
}

func TestAssessCategoryNoise(t *testing.T) {
	level := map[int64]string{1: NoiseLoud, 2: NoiseQuiet, 3: NoiseModerate}
	at := func(facade int64, _ int) (string, bool) { v, ok := level[facade]; return v, ok }
	ps := []Placement{{FacadeID: 1, FloorFrom: 1, FloorTo: 5, Rooms: 10}, {FacadeID: 2, FloorFrom: 1, FloorTo: 3, Rooms: 6}, {FacadeID: 3, FloorFrom: 2, FloorTo: 3, Rooms: 4}}
	a := AssessCategory(20, ps, at, NoiseQuiet)
	if !approx(a.Shares[NoiseLoud], 0.5) || !approx(a.Shares[NoiseQuiet], 0.3) || !approx(a.Shares[NoiseModerate], 0.2) {
		t.Errorf("доли шума: %v", a.Shares)
	}
	if a.Wish == nil || a.Wish.FacadeID != 2 || a.Wish.MinFloor != 0 {
		t.Errorf("пожелание «тихо»: %+v, ждали фасад 2 без этажа", a.Wish)
	}
}

func TestTitleClaimsSeaView(t *testing.T) {
	for name, want := range map[string]bool{
		"Sea View": true, "Deluxe Sea View Room": true, "Vista mar": true, "Habitación vista al mar": true, "SEAVIEW suite": true,
		"Standard": false, "Garden View": false, "Superior": false,
	} {
		if got := TitleClaimsSeaView(name); got != want {
			t.Errorf("%q: %v, ждали %v", name, got, want)
		}
	}
}

func TestCategoryRuleFacadeValueAt(t *testing.T) {
	sea := CategoryRule{Values: []string{"full", "none"}, Target: "full", FloorField: "min_floor"}
	for _, tt := range []struct {
		claimed map[string]any
		floor   int
		want    string
		ok      bool
	}{
		{map[string]any{"min_floor": float64(4)}, 4, "full", true},
		{map[string]any{"min_floor": float64(4)}, 3, "none", true},
		{map[string]any{"min_floor": nil}, 6, "none", true},
		{map[string]any{"levels": float64(5)}, 6, "", false},
	} {
		got, ok := sea.FacadeValueAt(tt.claimed, "min_floor", tt.floor)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%v на этаже %d: %q %v, ждали %q %v", tt.claimed, tt.floor, got, ok, tt.want, tt.ok)
		}
	}
	noise := CategoryRule{Values: []string{NoiseQuiet, NoiseModerate, NoiseLoud}, Target: NoiseQuiet}
	if v, ok := noise.FacadeValueAt(map[string]any{"level": NoiseModerate}, "level", 3); v != NoiseModerate || !ok {
		t.Errorf("шум: %q %v", v, ok)
	}
	if _, ok := noise.FacadeValueAt(map[string]any{"level": "deafening"}, "level", 3); ok {
		t.Error("значение вне values не должно приниматься")
	}
}

func seaCategoryEv(id int64, author Author, source string, rel float64, view string) Evidence {
	return Evidence{
		ID: id, EntityType: EntityRoomCategory, EntityID: "7", Key: SeaViewKey, Source: source, Reliability: rel,
		AuthorType: author.Type, AuthorID: author.ID, ObservedAt: t0,
		Claimed: map[string]any{"view": view}, Details: map[string]any{},
	}
}

// Название «Sea View» — утверждение отельера в свою пользу: надёжность 0,8 × 0,75 = 0,6, вклад ln(0,6/0,4) = 0,405.
// Расчёт по фасадам против (0,85, вклад 1,735) — конфликт; расчёт за — согласие без конфликта.
func TestSeaViewCategoryConflict(t *testing.T) {
	rules := map[string]TruthParams{SeaViewKey: {
		Kind: TruthSingle,
		ByEntity: map[string]TruthParams{EntityRoomCategory: {
			Kind: TruthEnum, ValueField: "view", FavorableValues: []string{"full"}, ConflictMinSide: 0.3,
		}},
	}}
	c := TruthContext{Now: t0, Rules: rules}
	claim := seaCategoryEv(1, HotelierAuthor(12), SourceHotelier, HotelierBaseReliability, "full")

	f, _ := AssembleFact([]Evidence{claim, seaCategoryEv(2, ToolAuthor(RoomProjectionMethod), SourceComputed, 0.85, "none")}, c)
	if f.Conflict == nil || f.Value["view"] != "none" {
		t.Fatalf("приукрашивание: значение %v, конфликт %v — ждали none и конфликт", f.Value, f.Conflict)
	}
	near(t, "вклад названия", f.Contributions[0].Value, -0.4055)
	near(t, "уверенность", f.Confidence, 0.7908)

	f, _ = AssembleFact([]Evidence{claim, seaCategoryEv(2, ToolAuthor(RoomProjectionMethod), SourceComputed, 0.85, "full")}, c)
	if f.Conflict != nil || f.WeakDisagreement || f.Value["view"] != "full" {
		t.Errorf("честное название: значение %v, конфликт %v, weak %v", f.Value, f.Conflict, f.WeakDisagreement)
	}
	if f.Confidence <= 0.85 {
		t.Errorf("согласие должно поднимать уверенность выше 0,85, получено %.3f", f.Confidence)
	}

	// Без правила категории (ByEntity) значение в поле view не читалось бы и конфликта не было: правило обязательно.
	f, _ = AssembleFact([]Evidence{claim, seaCategoryEv(2, ToolAuthor(RoomProjectionMethod), SourceComputed, 0.85, "none")},
		TruthContext{Now: t0, Rules: map[string]TruthParams{SeaViewKey: {Kind: TruthSingle}}})
	if f.Conflict != nil {
		t.Error("без правила категории конфликт невозможен")
	}
}

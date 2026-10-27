package domain

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func noiseEv(id int64, entityType, source string, rel float64, level string) Evidence {
	return Evidence{
		ID: id, EntityType: entityType, EntityID: "1", Key: NoiseNightKey, Source: source, Reliability: rel,
		AuthorType: AuthorTool, AuthorID: source + "@1", ObservedAt: t0,
		Claimed: map[string]any{"level": level}, Details: map[string]any{},
	}
}

func hotelierEv(id int64, rel float64, level string) Evidence {
	e := noiseEv(id, EntityFacade, "hotelier", rel, level)
	e.AuthorType, e.AuthorID = AuthorHotelier, "h1"
	return e
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("%s = %.4f, want %.4f", name, got, want)
	}
}

// testRules — правила характеристик, как в registry/*.yaml (их равенство проверяет тест загрузчика реестра).
var testRules = map[string]TruthParams{
	NoiseNightKey: {
		Kind: TruthEnum, ValueField: "level", Order: []string{NoiseQuiet, NoiseModerate, NoiseLoud},
		ConflictSteps: 2, ConflictMinSide: DefaultConflictMinSide,
		FavorableValues: []string{NoiseQuiet}, ConflictEntityTypes: []string{EntityFacade},
	},
	WalkToBeachKey: {Kind: TruthNumber, ValueField: "minutes", SignificantDiff: 5, ConflictMinSide: DefaultConflictMinSide},
	WalkToMetroKey: {Kind: TruthNumber, ValueField: "minutes", SignificantDiff: 5, ConflictMinSide: DefaultConflictMinSide},
}

func ctx() TruthContext { return TruthContext{Now: t0, Rules: testRules} }

// Расчёт по карте (0.85) и слабая оценка OSM (0.6) на разных ступенях: L = 1.7346 и 0.4055,
// S(quiet) = 1.7346 − 0.4055 = 1.3291, уверенность σ(1.3291) = 0.7908. «Против» весит 0.41 < θ=1: не конфликт, но флаг.
func TestTruthWeakDisagreement(t *testing.T) {
	bcn := noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseQuiet)
	osm := noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud)
	f, ok := AssembleFact([]Evidence{bcn, osm}, ctx())
	if !ok {
		t.Fatal("факта нет")
	}
	if f.Value["level"] != NoiseQuiet || f.Source != SourceBCNNoise {
		t.Errorf("значение/источник: %v %s", f.Value, f.Source)
	}
	near(t, "confidence", f.Confidence, 0.7908)
	near(t, "вклад BCN", f.Contributions[0].Value, 1.7346)
	near(t, "вклад OSM", f.Contributions[1].Value, -0.4055)
	if f.Conflict != nil || !f.WeakDisagreement {
		t.Errorf("конфликт=%v weak=%v, want weak без конфликта", f.Conflict, f.WeakDisagreement)
	}
	if len(f.EvidenceIDs) != 2 {
		t.Errorf("evidence_ids = %v", f.EvidenceIDs)
	}
}

// Расхождение на одну ступень только снижает уверенность (conflict_steps = 2).
func TestTruthOneStepOnlyLowersConfidence(t *testing.T) {
	f, _ := AssembleFact([]Evidence{
		noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseModerate),
		noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud),
	}, ctx())
	near(t, "confidence", f.Confidence, 0.7908)
	if f.Conflict != nil || f.WeakDisagreement {
		t.Errorf("одна ступень: конфликт=%v weak=%v", f.Conflict, f.WeakDisagreement)
	}
}

// Согласие складывается: S = 1.7346 + 0.4055 = 2.1401, σ = 0.8947 — выше любого из источников.
func TestTruthAgreementAdds(t *testing.T) {
	f, _ := AssembleFact([]Evidence{
		noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud),
		noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud),
	}, ctx())
	near(t, "confidence", f.Confidence, 0.8947)
	if f.Conflict != nil || f.WeakDisagreement {
		t.Error("согласие не даёт расхождения")
	}
}

// Отельер доверенный (trust 0.9 → ×1.4) говорит «тихо» против расчёта «громко»:
// r = 0.8·1.4 = 1.12, ×0.75 (выгодное значение) = 0.84, L = ln(0.84/0.16) = 1.6582.
// За громко 1.7346, против 1.6582, оба ≥ 1 — конфликт. Значение по расчёту, S = 0.0764, σ = 0.5191.
func TestTruthConflictHotelierAgainstTool(t *testing.T) {
	c := ctx()
	c.Trust = map[Author]float64{{AuthorHotelier, "h1"}: 0.9}
	f, _ := AssembleFact([]Evidence{
		noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud),
		hotelierEv(2, 0.8, NoiseQuiet),
	}, c)
	near(t, "r отельера", f.Contributions[1].Reliability, 0.84)
	near(t, "вклад отельера", f.Contributions[1].Value, -1.6582)
	near(t, "confidence", f.Confidence, 0.5191)
	if f.Conflict == nil || f.WeakDisagreement {
		t.Fatalf("want конфликт: %+v", f)
	}
	if f.Value["level"] != NoiseLoud {
		t.Errorf("значение не меняется от одного голоса: %v", f.Value)
	}
}

// Асимметрия: «тихо» от отельера слабее, чем признание «громко» с той же исходной надёжностью.
func TestTruthAsymmetry(t *testing.T) {
	p := testRules[NoiseNightKey]
	quiet := effectiveReliability(hotelierEv(1, 0.8, NoiseQuiet), p, ctx())
	loud := effectiveReliability(hotelierEv(2, 0.8, NoiseLoud), p, ctx())
	near(t, "выгодное", quiet, 0.6)
	near(t, "признание недостатка", loud, 0.8)
}

// Репутация: множитель 0.5 + trust. Новичок (0.5) ×1.0, нарушитель (0.1) ×0.6, доверенный (0.9) ×1.4, образцовый ×1.5.
func TestTruthReputation(t *testing.T) {
	p := testRules[NoiseNightKey]
	for _, tt := range []struct {
		trust, want float64
	}{{0.5, 0.6}, {0.1, 0.36}, {0.9, 0.84}, {1.0, 0.9}} {
		c := ctx()
		c.Trust = map[Author]float64{{AuthorHotelier, "h1"}: tt.trust}
		// «тихо» выгодно: 0.8 · (0.5+trust) · 0.75
		near(t, "r", effectiveReliability(hotelierEv(1, 0.8, NoiseQuiet), p, c), tt.want)
	}
}

// Из группы независимости берётся сильнейшее: второй набор карты (0.8) не добавляет вклад и не считается «против».
func TestTruthIndependenceGroup(t *testing.T) {
	a := noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud)
	b := noiseEv(2, EntityFacade, SourceBCNNoise, 0.8, NoiseQuiet)
	a.Details["independence_group"], b.Details["independence_group"] = "bcn_noise_2017", "bcn_noise_2017"
	f, _ := AssembleFact([]Evidence{a, b}, ctx())
	near(t, "confidence", f.Confidence, 0.85)
	if f.Contributions[1].Value != 0 || f.Conflict != nil || f.WeakDisagreement {
		t.Errorf("зависимое свидетельство должно не учитываться: %+v", f.Contributions)
	}
}

// Свежесть: середина срока — множитель 0.75 (0.8 → 0.6); после срока 0.5 (0.8 → 0.4) и признак stale.
func TestTruthFreshness(t *testing.T) {
	until := t0.Add(100 * 24 * time.Hour)
	e := noiseEv(1, EntityFacade, SourceBCNNoise, 0.8, NoiseLoud)
	e.ValidUntil = &until

	c := TruthContext{Now: t0.Add(50 * 24 * time.Hour), Rules: testRules}
	f, _ := AssembleFact([]Evidence{e}, c)
	near(t, "confidence на середине срока", f.Confidence, 0.6)
	if f.Stale {
		t.Error("в срок годности факт не устарел")
	}

	c.Now = t0.Add(200 * 24 * time.Hour)
	f, _ = AssembleFact([]Evidence{e}, c)
	// надёжность ниже 0.5 информации не несёт: для enum уверенность 0.5, сама поправленная r — 0.4
	near(t, "r после срока", f.Contributions[0].Reliability, 0.4)
	near(t, "confidence после срока", f.Confidence, 0.5)
	if !f.Stale {
		t.Error("после valid_until факт устарел")
	}
}

// Уровень отеля производный от фасада и своего конфликта или флага не даёт.
func TestTruthHotelLevelNoDisagreementFlags(t *testing.T) {
	f, _ := AssembleFact([]Evidence{
		noiseEv(1, EntityHotel, SourceBCNNoise, 0.85, NoiseQuiet),
		noiseEv(2, EntityHotel, SourceOSM, 0.6, NoiseLoud),
	}, ctx())
	if f.Conflict != nil || f.WeakDisagreement {
		t.Error("у отеля нет конфликтов")
	}
}

// Параметры θ и conflict_steps можно переопределить (таблица чувствительности collect truth).
func TestTruthRulesOverride(t *testing.T) {
	p := testRules[NoiseNightKey]
	p.ConflictMinSide = 0.4
	c := TruthContext{Now: t0, Rules: map[string]TruthParams{NoiseNightKey: p}}
	f, _ := AssembleFact([]Evidence{
		noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseQuiet),
		noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud),
	}, c)
	if f.Conflict == nil {
		t.Error("при θ=0.4 вес 0.41 уже конфликт")
	}
	p.ConflictSteps = 1
	c.Rules[NoiseNightKey] = p
	f, _ = AssembleFact([]Evidence{
		noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseModerate),
		noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud),
	}, c)
	if f.Conflict == nil {
		t.Error("при conflict_steps=1 ступень уже «против»")
	}
}

func walkEv(id int64, author string, rel, minutes float64) Evidence {
	e := Evidence{
		ID: id, EntityType: EntityHotel, EntityID: "1", Key: WalkToBeachKey, Source: SourceOSM, Reliability: rel,
		AuthorType: AuthorTool, AuthorID: "walk@1", ObservedAt: t0,
		Claimed: map[string]any{"minutes": minutes}, Details: map[string]any{},
	}
	if author != "" {
		e.AuthorType, e.AuthorID, e.Source = author, "h1", author
	}
	return e
}

// Числа не голосуют: значение из расчёта (10 мин), ответ отельера (25 мин, расхождение > 5) — проверка.
// За 1.7346, против 1.3863 (r=0.8), оба ≥ 1 — конфликт; S = 0.3483, σ = 0.5862.
func TestTruthNumberConflict(t *testing.T) {
	f, _ := AssembleFact([]Evidence{walkEv(1, "", 0.85, 10), walkEv(2, AuthorHotelier, 0.8, 25)}, ctx())
	if f.Value["minutes"] != 10.0 || f.Conflict == nil {
		t.Errorf("значение %v конфликт %v", f.Value, f.Conflict)
	}
	near(t, "confidence", f.Confidence, 0.5862)
	near(t, "вклад отельера", f.Contributions[1].Value, -1.3863)
}

// Расхождение в пределах significant_diff — подтверждение: S = 1.7346 + 1.3863 = 3.1209, σ = 0.9577.
func TestTruthNumberWithinSignificantDiff(t *testing.T) {
	f, _ := AssembleFact([]Evidence{walkEv(1, "", 0.85, 10), walkEv(2, AuthorHotelier, 0.8, 12)}, ctx())
	near(t, "confidence", f.Confidence, 0.9577)
	if f.Conflict != nil || f.WeakDisagreement {
		t.Error("в пределах significant_diff расхождения нет")
	}
}

// Число отельера, если расчёта нет, становится значением.
func TestTruthNumberWithoutTool(t *testing.T) {
	f, _ := AssembleFact([]Evidence{walkEv(1, AuthorHotelier, 0.8, 7)}, ctx())
	if f.Value["minutes"] != 7.0 {
		t.Errorf("значение %v", f.Value)
	}
}

// Характеристика без правил: сильнейшее свидетельство, при равенстве — более раннее.
func TestTruthSingleStrongest(t *testing.T) {
	a := Evidence{ID: 1, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.7, ObservedAt: t0, Claimed: map[string]any{"v": "a"}}
	b := Evidence{ID: 2, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.7, ObservedAt: t0, Claimed: map[string]any{"v": "b"}}
	c := Evidence{ID: 3, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.9, ObservedAt: t0, Claimed: map[string]any{"v": "c"}}
	f, _ := AssembleFact([]Evidence{a, b}, ctx())
	if f.Value["v"] != "a" {
		t.Errorf("при равенстве берётся более раннее: %v", f.Value)
	}
	f, _ = AssembleFact([]Evidence{a, c}, ctx())
	near(t, "confidence", f.Confidence, 0.9)
	if f.Value["v"] != "c" || f.Conflict != nil {
		t.Errorf("сильнейшее: %v", f.Value)
	}
}

func TestAssembleFactsGroupsByEntityAndKey(t *testing.T) {
	a := Evidence{ID: 1, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.8}
	b := Evidence{ID: 2, EntityType: EntityFacade, EntityID: "2", Key: SeaViewKey, Reliability: 0.8}
	c := Evidence{ID: 3, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.9}
	fs := AssembleFacts([]Evidence{a, b, c}, ctx())
	if len(fs) != 2 || fs[0].EntityID != "1" || len(fs[0].EvidenceIDs) != 2 || fs[1].EntityID != "2" {
		t.Errorf("факты: %+v", fs)
	}
	if got, ok := AssembleFact(nil, ctx()); ok || got.Key != "" {
		t.Error("без свидетельств факта нет")
	}
}

func catEv(id int64, author Author, rel float64, view string) Evidence {
	return Evidence{ID: id, EntityType: EntityRoomCategory, EntityID: "1", Key: SeaViewKey, Claimed: map[string]any{"view": view},
		AuthorType: author.Type, AuthorID: author.ID, Reliability: rel, ObservedAt: t0}
}

func catCtx() TruthContext {
	return TruthContext{Now: t0, Rules: map[string]TruthParams{SeaViewKey: {
		Kind: TruthSingle, ValueField: "min_floor",
		ByEntity: map[string]TruthParams{EntityRoomCategory: {
			Kind: TruthEnum, ValueField: "view", FavorableValues: []string{"full"}, ConflictMinSideHotelier: 0.3,
		}},
	}}}
}

// Пониженный θ = 0,3 нужен только названию категории: «Sea View» отельера (≈0,41 после асимметрии) против расчёта
// «none» — конфликт.
func TestCategoryReducedThetaWhenHotelierClaimInDispute(t *testing.T) {
	f, _ := AssembleFact([]Evidence{
		catEv(1, ToolAuthor(RoomProjectionMethod), 0.85, "none"),
		catEv(2, Author{AuthorHotelier, "5"}, 0.8, "full"),
	}, catCtx())
	if f.Conflict == nil {
		t.Errorf("название отельера против расчёта должно давать конфликт: %+v", f)
	}
}

// Без свидетельства отельера действует обычный θ = 1,0: один гость с обычной репутацией (вес ≈0,85) против расчёта —
// только weak_disagreement, а не конфликт (раньше пониженный θ превращал его в конфликт в обход правила трёх гостей).
func TestCategoryOrdinaryThetaForGuestsWithoutHotelier(t *testing.T) {
	f, _ := AssembleFact([]Evidence{
		catEv(1, ToolAuthor(RoomProjectionMethod), 0.85, "full"),
		catEv(2, Author{AuthorGuest, "7"}, 0.7, "none"),
	}, catCtx())
	if f.Conflict != nil || !f.WeakDisagreement {
		t.Errorf("один гость против расчёта: конфликт=%v weak=%v", f.Conflict, f.WeakDisagreement)
	}
	// Три гостя: «против» весит больше θ = 1,0 — конфликт.
	f, _ = AssembleFact([]Evidence{
		catEv(1, ToolAuthor(RoomProjectionMethod), 0.85, "full"),
		catEv(2, Author{AuthorGuest, "7"}, 0.7, "none"),
		catEv(3, Author{AuthorGuest, "8"}, 0.7, "none"),
		catEv(4, Author{AuthorGuest, "9"}, 0.7, "none"),
	}, catCtx())
	if f.Conflict == nil {
		t.Error("три гостя против расчёта: ожидался конфликт")
	}
}

// Калибровка базы гостя (0,6): надёжный гость (репутация 0,67) против расчёта 0,85 в категории. Один гость — только
// weak_disagreement, двое — конфликт, трое — перевес на их стороне.
func TestGuestBaseCalibrationOneTwoThree(t *testing.T) {
	run := func(n int) Fact {
		ev := []Evidence{catEv(1, ToolAuthor(RoomProjectionMethod), 0.85, "full")}
		ctx := catCtx()
		ctx.Trust = map[Author]float64{}
		for i := 0; i < n; i++ {
			a := Author{AuthorGuest, string(rune('a' + i))}
			ctx.Trust[a] = 0.67
			ev = append(ev, catEv(int64(2+i), a, GuestBaseReliability, "none"))
		}
		f, _ := AssembleFact(ev, ctx)
		return f
	}
	if f := run(1); f.Conflict != nil || !f.WeakDisagreement || f.Value["view"] != "full" {
		t.Errorf("один гость: ожидался weak_disagreement без конфликта: %+v", f)
	}
	if f := run(2); f.Conflict == nil {
		t.Errorf("двое гостей: ожидался конфликт: %+v", f)
	}
	if f := run(3); f.Conflict == nil || f.Value["view"] != "none" {
		t.Errorf("трое гостей: ожидался конфликт, значение none: %+v", f)
	}
}

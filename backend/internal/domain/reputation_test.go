package domain

import (
	"math"
	"testing"
)

func repCtx() TruthContext {
	return TruthContext{Rules: map[string]TruthParams{
		"sea_view": {Kind: TruthSingle, ValueField: "min_floor", ByEntity: map[string]TruthParams{
			EntityRoomCategory: {Kind: TruthEnum, ValueField: "view", FavorableValues: []string{"full"}}}},
		"pets_allowed": {Kind: TruthEnum, ValueField: "available"},
		"noise_night":  {Kind: TruthEnum, ValueField: "level", Order: []string{"quiet", "moderate", "loud"}},
	}}
}

func catClaim(author string, view string) Evidence {
	return Evidence{EntityType: EntityRoomCategory, EntityID: "1", Key: "sea_view", Claimed: map[string]any{"view": view},
		AuthorType: AuthorHotelier, AuthorID: author, Reliability: 0.8}
}

func calc(view string, r float64) Evidence {
	return Evidence{EntityType: EntityRoomCategory, EntityID: "1", Key: "sea_view", Claimed: map[string]any{"view": view},
		AuthorType: AuthorTool, AuthorID: RoomProjectionMethod, Reliability: r}
}

func TestJudgeClaim(t *testing.T) {
	ctx := repCtx()
	// «Sea View» против расчёта «нет вида» с r = 0,85: расхождение засчитывается.
	v := JudgeClaim(catClaim("5", "full"), []Evidence{calc("none", 0.85)}, ctx)
	if !v.Counted || v.Agree || v.Against != "none" {
		t.Errorf("расхождение: %+v", v)
	}
	// Совпадение.
	if v := JudgeClaim(catClaim("5", "none"), []Evidence{calc("none", 0.85)}, ctx); !v.Counted || !v.Agree {
		t.Errorf("совпадение: %+v", v)
	}
	// Сильное расхождение не важнее слабого: порог только по надёжности судьи, ровно 0,8 подходит.
	if v := JudgeClaim(catClaim("5", "full"), []Evidence{calc("none", 0.8)}, ctx); !v.Counted || v.Agree {
		t.Errorf("r = 0,8: %+v", v)
	}
	// Слабый судья (r < 0,8) утверждение не оценивает.
	if v := JudgeClaim(catClaim("5", "full"), []Evidence{calc("none", 0.6)}, ctx); v.Counted {
		t.Errorf("r = 0,6 не судья: %+v", v)
	}
	// Своё же прежнее свидетельство судьёй не служит.
	own := catClaim("5", "none")
	own.Reliability = 0.97
	if v := JudgeClaim(catClaim("5", "full"), []Evidence{own}, ctx); v.Counted {
		t.Errorf("тот же автор: %+v", v)
	}
	// Из нескольких судей берётся сильнейший.
	if v := JudgeClaim(catClaim("5", "full"), []Evidence{calc("none", 0.82), calc("full", 0.9)}, ctx); !v.Agree {
		t.Errorf("сильнейший судья: %+v", v)
	}
	// Диапазон (вид отеля по этажам) и неизвестная характеристика не сравниваются.
	hotelLevel := Evidence{EntityType: EntityHotel, EntityID: "1", Key: "sea_view", Claimed: map[string]any{"min_floor": 3.0}, AuthorType: AuthorHotelier, AuthorID: "5"}
	if v := JudgeClaim(hotelLevel, []Evidence{calc("none", 0.9)}, ctx); v.Counted {
		t.Errorf("диапазон не сравнивается: %+v", v)
	}
}

// Упорядоченный enum: «умеренно» против «тихо» — расхождение, насколько бы оно ни было мало.
func TestJudgeClaimIgnoresDisagreementStrength(t *testing.T) {
	claim := Evidence{EntityType: EntityHotel, EntityID: "1", Key: "noise_night", Claimed: map[string]any{"level": "quiet"}, AuthorType: AuthorHotelier, AuthorID: "5"}
	other := Evidence{EntityType: EntityHotel, EntityID: "1", Key: "noise_night", Claimed: map[string]any{"level": "moderate"}, AuthorType: AuthorTool, AuthorID: "bcn", Reliability: 0.85}
	if v := JudgeClaim(claim, []Evidence{other}, repCtx()); !v.Counted || v.Agree {
		t.Errorf("соседние ступени: %+v", v)
	}
}

func TestTrustFromCounts(t *testing.T) {
	cases := []struct {
		agree, total int
		want         float64
	}{{0, 0, 0.5}, {0, 1, 1.0 / 3}, {1, 1, 2.0 / 3}, {3, 4, 4.0 / 6}, {0, 3, 0.2}}
	for _, c := range cases {
		if got := TrustFromCounts(c.agree, c.total); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%d/%d: %v, ожидалось %v", c.agree, c.total, got, c.want)
		}
	}
}

// Сценарий: приукрашивающий подтвердил вид → расхождение с расчётом → trust падает → его непроверяемые
// утверждения (питомцы) весят меньше, уверенность факта ниже.
func TestEmbellisherLosesWeightOnUnverifiable(t *testing.T) {
	ctx := repCtx()
	ctx.Rules["pets_allowed"] = TruthParams{Kind: TruthEnum, ValueField: "available", FavorableValues: []string{"true"}}
	pets := Evidence{EntityType: EntityHotel, EntityID: "5", Key: "pets_allowed", Claimed: map[string]any{"available": true},
		AuthorType: AuthorHotelier, AuthorID: "5", Reliability: HotelierBaseReliability}
	conf := func(trust float64) float64 {
		c := ctx
		c.Trust = map[Author]float64{{AuthorHotelier, "5"}: trust}
		fs := AssembleFacts([]Evidence{pets}, c)
		if len(fs) != 1 {
			t.Fatalf("фактов: %d", len(fs))
		}
		return fs[0].Confidence
	}
	before := conf(DefaultTrust)
	v := JudgeClaim(catClaim("5", "full"), []Evidence{calc("none", 0.85)}, ctx)
	trust := TrustFromCounts(map[bool]int{true: 1, false: 0}[v.Agree], 1)
	after := conf(trust)
	if !(trust < DefaultTrust) || !(after < before) {
		t.Fatalf("trust %.2f → %.2f, уверенность %.3f → %.3f", DefaultTrust, trust, before, after)
	}
	t.Logf("trust %.2f → %.2f; уверенность «питомцы разрешены» %.3f → %.3f", DefaultTrust, trust, before, after)
}

package domain

import (
	"math"
	"strings"
	"testing"
)

// Цепочка надёжности обязана давать ту же r, что и движок (effectiveReliability), на всех сочетаниях множителей.
func TestReliabilityChainProductEqualsEngine(t *testing.T) {
	c := ctx()
	c.Trust = map[Author]float64{{AuthorHotelier, "h1"}: 0.33, {AuthorGuest, "g1"}: 0.9}
	until := t0.AddDate(0, 6, 0)
	evs := []Evidence{
		noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud),
		hotelierEv(2, 0.8, NoiseQuiet), // в пользу отеля: асимметрия
		hotelierEv(3, 0.8, NoiseLoud),  // признание недостатка
		{ID: 4, EntityType: EntityFacade, EntityID: "1", Key: NoiseNightKey, Claimed: map[string]any{"level": NoiseLoud},
			AuthorType: AuthorGuest, AuthorID: "g1", Reliability: 0.7, ObservedAt: t0.AddDate(-1, 0, 0), ValidUntil: &until},
		noiseEv(5, EntityFacade, SourceOSM, 0.99, NoiseModerate), // ограничение сверху
	}
	p := c.params(NoiseNightKey, EntityFacade)
	for _, e := range evs {
		ch := reliabilityChain(e, p, c)
		if want := effectiveReliability(e, p, c); ch.R != want {
			t.Errorf("свидетельство %d: R = %v, движок %v", e.ID, ch.R, want)
		}
		prod := ch.Base * ch.TrustMult * ch.Asymmetry * ch.Freshness
		if math.Abs(prod-ch.Product) > 1e-12 {
			t.Errorf("свидетельство %d: произведение множителей %v, Product %v", e.ID, prod, ch.Product)
		}
	}
	if ch := reliabilityChain(evs[4], p, c); ch.R != MaxReliability || ch.Product <= MaxReliability {
		t.Errorf("ограничение сверху: %+v", ch)
	}
	if ch := reliabilityChain(evs[1], p, c); ch.Asymmetry != HotelierAsymmetry || math.Abs(ch.TrustMult-0.83) > 1e-12 {
		t.Errorf("отельер в свою пользу: %+v", ch)
	}
}

// ExplainFact раскладывает тот же факт, что AssembleFact: вклады и уверенность совпадают.
func TestExplainFactMatchesAssemble(t *testing.T) {
	c := ctx()
	es := []Evidence{noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud), hotelierEv(2, 0.8, NoiseQuiet)}
	f, _ := AssembleFact(es, c)
	x, ok := ExplainFact(es, c)
	if !ok || x.Fact.Confidence != f.Confidence {
		t.Fatalf("факт: %+v", x.Fact)
	}
	var sum float64
	for i, e := range x.Evidence {
		if e.Contribution != f.Contributions[i].Value {
			t.Errorf("вклад %d: %v и %v", i, e.Contribution, f.Contributions[i].Value)
		}
		sum += e.Contribution
	}
	if math.Abs(x.S-sum) > 1e-12 || math.Abs(x.Sigma-sigmoid(sum)) > 1e-12 {
		t.Errorf("S = %v, σ = %v", x.S, x.Sigma)
	}
}

// Свидетельство отельера с репутацией 0,33 в свою пользу: 0,8 · 0,83 · 0,75 = 0,498 — вклад 0, пометка.
func TestExplainFactZeroWeightAndSummary(t *testing.T) {
	c := ctx()
	c.Trust = map[Author]float64{{AuthorHotelier, "h1"}: 1.0 / 3}
	es := []Evidence{noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud), hotelierEv(2, 0.8, NoiseQuiet)}
	x, _ := ExplainFact(es, c)
	if !x.Evidence[1].ZeroWeight || x.Evidence[0].ZeroWeight {
		t.Errorf("нулевой вес: %+v", x.Evidence)
	}
	if !strings.Contains(x.Summary, "отельер ≈0") || !strings.Contains(x.Summary, "карта шума +") {
		t.Errorf("сводка: %q", x.Summary)
	}
}

func TestExplainFactDependentSourceAndGuestsSummary(t *testing.T) {
	c := ctx()
	a := noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud)
	b := noiseEv(2, EntityFacade, SourceBCNNoise, 0.7, NoiseLoud)
	a.Details = map[string]any{"independence_group": "bcn_2017"}
	b.Details = map[string]any{"independence_group": "bcn_2017"}
	g1, g2 := hotelierEv(3, 0.7, NoiseQuiet), hotelierEv(4, 0.7, NoiseQuiet)
	g1.AuthorType, g1.AuthorID, g2.AuthorType, g2.AuthorID = AuthorGuest, "g1", AuthorGuest, "g2"
	x, _ := ExplainFact([]Evidence{a, b, g1, g2}, c)
	if x.Evidence[1].Role != RoleIgnoredD || x.Evidence[1].Group != "bcn_2017" {
		t.Errorf("зависимый источник: %+v", x.Evidence[1])
	}
	if !strings.Contains(x.Summary, "2 гостя −") {
		t.Errorf("сводка: %q", x.Summary)
	}
}

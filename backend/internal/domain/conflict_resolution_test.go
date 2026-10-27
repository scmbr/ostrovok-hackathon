package domain

import (
	"strings"
	"testing"
)

func closedConflict(t *testing.T, trust float64, es ...Evidence) ConflictResolution {
	t.Helper()
	c := ctx()
	c.Trust = map[Author]float64{{Type: AuthorHotelier, ID: "h1"}: trust}
	f, ok := AssembleFact(es, c)
	if !ok {
		t.Fatal("факта нет")
	}
	if f.Conflict != nil {
		t.Fatalf("конфликт должен быть закрыт: %s", f.Conflict.Description)
	}
	return ExplainClosedConflict(f, es, c)
}

// Расчёт «громко» (0,85) против отельера «тихо»: при репутации 0,33 надёжность его слова 0,8·0,83·0,75 = 0,498 ≤ 0,5,
// вес нулевой. Конфликт исчез не потому, что стороны сошлись.
func TestClosedConflictWeightDropped(t *testing.T) {
	r := closedConflict(t, 0.33, noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud), hotelierEv(2, 0.8, NoiseQuiet))
	if r.Kind != ResolutionWeightDropped || r.Author.Type != AuthorHotelier || r.Reliability > 0.5 {
		t.Fatalf("причина: %+v", r)
	}
	if !strings.Contains(r.Text, "в пользу расчёта") || !strings.Contains(r.Text, "0,33") {
		t.Errorf("текст: %q", r.Text)
	}
}

func TestClosedConflictAgreed(t *testing.T) {
	r := closedConflict(t, 0.5, noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud), hotelierEv(2, 0.8, NoiseLoud))
	if r.Kind != ResolutionAgreed {
		t.Fatalf("причина: %+v", r)
	}
}

func TestClosedConflictBelowThreshold(t *testing.T) {
	r := closedConflict(t, 0.5, noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseQuiet), noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud))
	if r.Kind != ResolutionBelowThreshold {
		t.Fatalf("причина: %+v", r)
	}
}

// Репутация 1/3 даёт надёжность ровно 0,5 с точностью до плавающей запятой (сценарий «Sea View» отеля 326): вес всё равно нулевой.
func TestClosedConflictWeightDroppedAtExactlyHalf(t *testing.T) {
	r := closedConflict(t, 1.0/3, noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud), hotelierEv(2, 0.8, NoiseQuiet))
	if r.Kind != ResolutionWeightDropped {
		t.Fatalf("причина: %+v", r)
	}
}

package domain

import (
	"testing"
	"time"
)

func noiseEv(id int64, entityType, source string, rel float64, level string, ln float64) Evidence {
	return Evidence{
		ID: id, EntityType: entityType, EntityID: "1", Key: NoiseNightKey, Source: source, Reliability: rel,
		Claimed: map[string]any{"level": level, "ln_db": ln, "dominant": "road:secondary"},
	}
}

func TestAssembleFactTakesStrongestEvidence(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Hour)
	osm := noiseEv(1, EntityFacade, SourceOSM, 0.6, NoiseLoud, 62)
	bcn := noiseEv(2, EntityFacade, SourceBCNNoise, 0.85, NoiseQuiet, 38)
	bcn.Method, bcn.ObservedAt, bcn.ValidUntil = NoiseNightBCNMethod, now, &until

	f, ok := AssembleFact([]Evidence{osm, bcn})
	if !ok {
		t.Fatal("факта нет")
	}
	if f.Value["level"] != NoiseQuiet || f.Source != SourceBCNNoise || f.Method != NoiseNightBCNMethod ||
		f.Confidence != 0.85 || !f.ObservedAt.Equal(now) || f.ValidUntil == nil {
		t.Errorf("факт должен быть по сильнейшему свидетельству: %+v", f)
	}
	if len(f.EvidenceIDs) != 2 || f.EvidenceIDs[0] != 1 || f.EvidenceIDs[1] != 2 {
		t.Errorf("evidence_ids = %v, want [1 2]", f.EvidenceIDs)
	}
	if f.Conflict == nil {
		t.Error("тихо по карте и громко по OSM — конфликт фасада")
	}
}

func TestAssembleFactTieKeepsEarlier(t *testing.T) {
	a := noiseEv(1, EntityFacade, SourceBCNNoise, 0.7, NoiseQuiet, 40)
	b := noiseEv(2, EntityFacade, SourceOSM, 0.7, NoiseModerate, 50)
	if f, _ := AssembleFact([]Evidence{a, b}); f.Source != SourceBCNNoise {
		t.Errorf("при равной надёжности берётся более раннее: %s", f.Source)
	}
}

func TestAssembleFactConflicts(t *testing.T) {
	tests := []struct {
		name string
		es   []Evidence
		want bool
	}{
		{"согласны", []Evidence{noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseLoud, 60), noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud, 62)}, false},
		{"расходятся на ступень", []Evidence{noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseModerate, 50), noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud, 62)}, true},
		{"только одно свидетельство", []Evidence{noiseEv(1, EntityFacade, SourceOSM, 0.6, NoiseLoud, 62)}, false},
		{"отель: конфликт у фасада", []Evidence{noiseEv(1, EntityHotel, SourceBCNNoise, 0.85, NoiseQuiet, 38), noiseEv(2, EntityHotel, SourceOSM, 0.6, NoiseLoud, 62)}, false},
	}
	for _, tt := range tests {
		f, _ := AssembleFact(tt.es)
		if (f.Conflict != nil) != tt.want {
			t.Errorf("%s: конфликт = %v, want %v", tt.name, f.Conflict, tt.want)
		}
	}
	f, _ := AssembleFact([]Evidence{noiseEv(1, EntityFacade, SourceBCNNoise, 0.85, NoiseModerate, 50), noiseEv(2, EntityFacade, SourceOSM, 0.6, NoiseLoud, 62)})
	want := "карта шума BCN: moderate (Ln 50.0 дБ); оценка по OSM: loud (Ln 62.0 дБ, главный источник road:secondary)"
	if f.Conflict.Description != want {
		t.Errorf("описание конфликта: %q", f.Conflict.Description)
	}
}

func TestAssembleFactsGroupsByEntityAndKey(t *testing.T) {
	a := Evidence{ID: 1, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.8}
	b := Evidence{ID: 2, EntityType: EntityFacade, EntityID: "2", Key: SeaViewKey, Reliability: 0.8}
	c := Evidence{ID: 3, EntityType: EntityFacade, EntityID: "1", Key: SeaViewKey, Reliability: 0.9}
	fs := AssembleFacts([]Evidence{a, b, c})
	if len(fs) != 2 || fs[0].EntityID != "1" || len(fs[0].EvidenceIDs) != 2 || fs[1].EntityID != "2" {
		t.Errorf("факты: %+v", fs)
	}
	if got, ok := AssembleFact(nil); ok || got.Key != "" {
		t.Error("без свидетельств факта нет")
	}
}

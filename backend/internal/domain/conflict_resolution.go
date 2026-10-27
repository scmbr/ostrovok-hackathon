package domain

import (
	"fmt"
	"math"
	"strings"
)

// Причины, по которым ранее открытый конфликт больше не открывается (conflicts.resolution_kind).
const (
	// ResolutionAgreed — все свидетельства теперь за одно значение: стороны сошлись.
	ResolutionAgreed = "agreed"
	// ResolutionWeightDropped — противоположная сторона осталась, но её вес стал нулевым (надёжность не выше 0,5 информации
	// не несёт), чаще всего из-за упавшего доверия к автору. Спор снят в пользу другой стороны, а не «сошёлся».
	ResolutionWeightDropped = "weight_dropped"
	// ResolutionBelowThreshold — противоположная сторона ещё весит, но меньше порога θ: остаётся weak_disagreement.
	ResolutionBelowThreshold = "below_threshold"
	// ResolutionEvidenceRemoved — свидетельства сущности сняты, факта больше нет.
	ResolutionEvidenceRemoved = "evidence_removed"
)

// ConflictResolution — почему конфликт закрылся; пишется в conflicts.resolution (текст) и resolution_kind.
type ConflictResolution struct {
	Kind string
	Text string
	// Author и Reliability — чей вес упал (только weight_dropped).
	Author      Author
	Reliability float64
}

// ExplainClosedConflict объясняет, почему у пересобранного факта f больше нет конфликта, который был у прежнего.
// es — активные свидетельства сущности, из которых собран f. Только чтение: ничего не пересчитывает, берёт вклады
// и надёжности, уже посчитанные движком (f.Contributions).
func ExplainClosedConflict(f Fact, es []Evidence, c TruthContext) ConflictResolution {
	p := c.params(f.Key, f.EntityType)
	want := fmt.Sprint(f.Value[p.ValueField])
	byID := make(map[int64]Evidence, len(es))
	for _, e := range es {
		byID[e.ID] = e
	}
	dissent := false
	var dropped *Contribution
	var droppedEv Evidence
	for i, ct := range f.Contributions {
		e, ok := byID[ct.EvidenceID]
		if !ok || claimedString(e, p) == want {
			continue
		}
		dissent = true
		// Приоритет — свидетельство человека: именно его вес упал вместе с репутацией.
		if math.Abs(ct.Value) < 0.01 && ct.Reliability <= 0.5+1e-6 && (dropped == nil || e.AuthorType != AuthorTool) {
			dropped, droppedEv = &f.Contributions[i], e
		}
	}
	switch {
	case !dissent:
		return ConflictResolution{Kind: ResolutionAgreed, Text: fmt.Sprintf("стороны сошлись: все свидетельства за «%s»", want)}
	case dropped != nil:
		a := Author{Type: droppedEv.AuthorType, ID: droppedEv.AuthorID}
		text := fmt.Sprintf("спор снят в пользу расчёта: вес утверждения «%s» (%s) стал нулевым, надёжность %s не несёт информации",
			claimedString(droppedEv, p), authorWord(a.Type), comma2(dropped.Reliability))
		if a.Type != AuthorTool {
			trust, ok := c.Trust[a]
			if !ok {
				trust = DefaultTrust
			}
			text += fmt.Sprintf(" (доверие к автору %s)", comma2(trust))
		}
		return ConflictResolution{Kind: ResolutionWeightDropped, Text: text, Author: a, Reliability: dropped.Reliability}
	}
	return ConflictResolution{Kind: ResolutionBelowThreshold, Text: "противоположная сторона весит меньше порога конфликта: осталось только расхождение"}
}

func authorWord(t string) string {
	switch t {
	case AuthorHotelier:
		return "отель"
	case AuthorGuest:
		return "гость"
	}
	return "расчёт"
}

func comma2(x float64) string { return strings.Replace(fmt.Sprintf("%.2f", x), ".", ",", 1) }

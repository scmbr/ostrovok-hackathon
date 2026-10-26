// Пакет truth пересобирает факты из активных свидетельств движком правды (domain.AssembleFacts).
package truth

import (
	"context"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Store — порт хранилища: свидетельства и репутация на вход, факты на выход.
type Store interface {
	ActiveEvidence(ctx context.Context) ([]domain.Evidence, error)
	AuthorTrust(ctx context.Context) (map[domain.Author]float64, error)
	ReplaceFacts(ctx context.Context, facts []domain.Fact) error
}

// Cell — число расхождений noise_night по фасадам при одной паре параметров.
type Cell struct {
	Theta     float64
	Steps     int
	Conflicts int
	Weak      int
}

// Report — итог пересборки.
type Report struct {
	Facts, Conflicts, Weak, Stale int
	// NoiseFacadeFacts — фактов noise_night по фасадам; NoiseTwoSources — из них с картой BCN и оценкой OSM.
	NoiseFacadeFacts, NoiseTwoSources int
	// Sensitivity — таблица по θ × conflict_steps для noise_night на фасадах.
	Sensitivity []Cell
}

// Параметры таблицы чувствительности.
var (
	SensitivityThetas = []float64{0.4, 0.8, 1.0}
	SensitivitySteps  = []int{1, 2}
)

// Recompute пересобирает все факты.
type Recompute struct {
	Store Store
	Now   func() time.Time
}

// Run собирает факты правилами по умолчанию (domain.TruthRules), записывает их и считает таблицу чувствительности.
func (r *Recompute) Run(ctx context.Context) (Report, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	evs, err := r.Store.ActiveEvidence(ctx)
	if err != nil {
		return Report{}, err
	}
	trust, err := r.Store.AuthorTrust(ctx)
	if err != nil {
		return Report{}, err
	}
	tc := domain.TruthContext{Now: now(), Trust: trust}
	facts := domain.AssembleFacts(evs, tc)
	if err := r.Store.ReplaceFacts(ctx, facts); err != nil {
		return Report{}, err
	}

	rep := Report{Facts: len(facts)}
	for _, f := range facts {
		if f.Conflict != nil {
			rep.Conflicts++
		}
		if f.WeakDisagreement {
			rep.Weak++
		}
		if f.Stale {
			rep.Stale++
		}
		if f.Key == domain.NoiseNightKey && f.EntityType == domain.EntityFacade {
			rep.NoiseFacadeFacts++
			if len(f.EvidenceIDs) > 1 {
				rep.NoiseTwoSources++
			}
		}
	}

	var noise []domain.Evidence
	for _, e := range evs {
		if e.Key == domain.NoiseNightKey && e.EntityType == domain.EntityFacade {
			noise = append(noise, e)
		}
	}
	for _, steps := range SensitivitySteps {
		for _, theta := range SensitivityThetas {
			p := domain.TruthRules[domain.NoiseNightKey]
			p.ConflictMinSide, p.ConflictSteps = theta, steps
			cell := Cell{Theta: theta, Steps: steps}
			tcv := domain.TruthContext{Now: tc.Now, Trust: trust, Rules: map[string]domain.TruthParams{domain.NoiseNightKey: p}}
			for _, f := range domain.AssembleFacts(noise, tcv) {
				if f.Conflict != nil {
					cell.Conflicts++
				}
				if f.WeakDisagreement {
					cell.Weak++
				}
			}
			rep.Sensitivity = append(rep.Sensitivity, cell)
		}
	}
	return rep, nil
}

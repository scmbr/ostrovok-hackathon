package compute

import (
	"context"
	"fmt"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// recorder — хранилище свидетельств в памяти для тестов инструментов: присваивает номера свидетельствам
// и собирает факты так же, как БД (domain.AssembleFacts). Ключ — «область/характеристика», например «7/noise_night».
type recorder struct {
	saved map[string][]domain.Fact
	byID  map[int64]domain.Evidence
}

func (r *recorder) record(scope, key string, evs []domain.Evidence) {
	if r.saved == nil {
		r.saved, r.byID = map[string][]domain.Fact{}, map[int64]domain.Evidence{}
	}
	for i := range evs {
		evs[i].ID = int64(len(r.byID) + 1)
		r.byID[evs[i].ID] = evs[i]
	}
	// «Сейчас» — момент расчёта (самое позднее наблюдение), чтобы свежесть не меняла уверенность в тестах.
	var now time.Time
	for _, e := range evs {
		if e.ObservedAt.After(now) {
			now = e.ObservedAt
		}
	}
	r.saved[scope+"/"+key] = domain.AssembleFacts(evs, domain.TruthContext{Now: now, Rules: testRules()})
}

func (r *recorder) SaveHotelEvidence(_ context.Context, hotelID int64, key string, _ []domain.Author, evs []domain.Evidence) error {
	r.record(fmt.Sprint(hotelID), key, evs)
	return nil
}

func (r *recorder) SaveDistrictEvidence(_ context.Context, district, key string, _ []domain.Author, evs []domain.Evidence) error {
	r.record(district, key, evs)
	return nil
}

// ev возвращает свидетельства, из которых сложен факт f.
func (r *recorder) ev(f domain.Fact) []domain.Evidence {
	out := make([]domain.Evidence, 0, len(f.EvidenceIDs))
	for _, id := range f.EvidenceIDs {
		out = append(out, r.byID[id])
	}
	return out
}

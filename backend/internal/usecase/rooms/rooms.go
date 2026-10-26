// Пакет rooms переносит характеристики с фасадов и этажей на категории номеров: вероятность, гарантия, пожелание.
// Островок продаёт категории, а факты у нас по фасадам и этажам; размещение номеров (категория → фасад, этажи,
// число номеров) сообщает отельер. Расчёт — domain.AssessCategory, здесь он применяется к данным отеля.
package rooms

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// ErrNotFound — у отеля нет категорий номеров или самого отеля.
var ErrNotFound = errors.New("отель или категории номеров не найдены")

// FacadeInfo — фасад отеля для показа размещения.
type FacadeInfo struct {
	ID          int64
	AzimuthDeg  float64
	FacesStreet bool
	Courtyard   bool
}

// EvidenceView — свидетельство, из которого сложен факт категории, с вкладом в итог.
type EvidenceView struct {
	ID           int64
	Source       string
	AuthorType   string
	AuthorID     string
	Claimed      map[string]any
	Reliability  float64
	Contribution float64
	ObservedAt   time.Time
}

// CategoryFact — итоговый факт категории номеров (движок правды) со свидетельствами и открытым конфликтом.
type CategoryFact struct {
	CategoryID int64
	Fact       domain.Fact
	Evidence   []EvidenceView
}

// Store — хранилище категорий номеров. Реализация — adapter/repository/postgres.
type Store interface {
	// HotelsWithCategories — отели с размещёнными категориями.
	HotelsWithCategories(ctx context.Context, flt domain.HotelFilter) ([]int64, error)
	Categories(ctx context.Context, hotelID int64) ([]domain.RoomCategory, error)
	Facades(ctx context.Context, hotelID int64) ([]FacadeInfo, error)
	// FacadeFacts — активные факты фасадов отеля по характеристикам keys.
	FacadeFacts(ctx context.Context, hotelID int64, keys []string) ([]domain.Fact, error)
	CategoryFacts(ctx context.Context, hotelID int64) ([]CategoryFact, error)
	// SaveCategoryEvidence записывает свидетельства о категории и пересобирает её факт; пустой evs снимает
	// прежние свидетельства авторов authors.
	SaveCategoryEvidence(ctx context.Context, categoryID int64, key string, authors []domain.Author, evs []domain.Evidence) error
}

// projection — характеристика, переносимая на категории, с фактами фасадов отеля.
type projection struct {
	char  domain.Characteristic
	rule  domain.CategoryRule
	facts map[int64]domain.Fact // по id фасада
}

// projections выбирает из реестра характеристики с правилом category и привязывает факты фасадов.
func projections(reg domain.Registry, facts []domain.Fact) []projection {
	var out []projection
	for _, c := range reg.All() {
		if c.Category == nil {
			continue
		}
		p := projection{char: c, rule: *c.Category, facts: map[int64]domain.Fact{}}
		for _, f := range facts {
			if f.Key != c.Key {
				continue
			}
			if id, ok := facadeID(f); ok {
				p.facts[id] = f
			}
		}
		out = append(out, p)
	}
	return out
}

func facadeID(f domain.Fact) (int64, bool) {
	id, err := strconv.ParseInt(f.EntityID, 10, 64)
	return id, err == nil
}

func projectionKeys(reg domain.Registry) []string {
	var keys []string
	for _, c := range reg.All() {
		if c.Category != nil {
			keys = append(keys, c.Key)
		}
	}
	return keys
}

// valueAt — значение характеристики у фасада на этаже по факту фасада.
func (p projection) valueAt(facadeID int64, floor int) (string, bool) {
	f, ok := p.facts[facadeID]
	if !ok {
		return "", false
	}
	return p.rule.FacadeValueAt(f.Value, p.char.FieldName(), floor)
}

// assess считает категорию по размещению и фактам фасадов.
func (p projection) assess(c domain.RoomCategory) domain.CategoryAssessment {
	return domain.AssessCategory(c.Rooms, c.Placed(), p.valueAt, p.rule.Target)
}

// confidence — средняя уверенность фактов фасадов, взвешенная числом номеров размещения; false — фактов нет.
func (p projection) confidence(c domain.RoomCategory) (float64, bool) {
	sum, weight := 0.0, 0.0
	for _, pl := range c.Placements {
		if f, ok := p.facts[pl.FacadeID]; ok {
			sum += f.Confidence * float64(pl.Rooms)
			weight += float64(pl.Rooms)
		}
	}
	if weight == 0 {
		return 0, false
	}
	return sum / weight, true
}

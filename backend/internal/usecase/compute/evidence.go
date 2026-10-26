package compute

import (
	"context"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// EvidenceSaver — запись результата инструмента. Инструменты пишут только свидетельства; факты и конфликты
// из активных свидетельств (включая ответы людей) собирает хранилище по domain.AssembleFacts.
// Пустой evs снимает прежние свидетельства авторов authors (например, маршрут больше не найден).
type EvidenceSaver interface {
	SaveHotelEvidence(ctx context.Context, hotelID int64, key string, authors []domain.Author, evs []domain.Evidence) error
}

// DistrictEvidenceSaver — то же для районных характеристик.
type DistrictEvidenceSaver interface {
	SaveDistrictEvidence(ctx context.Context, district, key string, authors []domain.Author, evs []domain.Evidence) error
}

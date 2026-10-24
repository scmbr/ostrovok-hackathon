package collect

import (
	"math/rand"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

var (
	// starWeights — доли отелей по звёздам (индекс 0 = 2 звезды).
	starWeights = []float64{0.10, 0.35, 0.40, 0.15}
	basePrice   = map[int]int{2: 60, 3: 95, 4: 150, 5: 260}
	roomPool    = []string{"Standard", "Superior", "Deluxe", "Family", "Suite"}
)

// MockCard строит мок базовой карточки. Результат зависит только от osmID,
// поэтому повторная генерация даёт те же значения.
func MockCard(osmID int64) domain.Card {
	rng := rand.New(rand.NewSource(osmID))

	stars := pickStars(rng.Float64())
	price := int(float64(basePrice[stars]) * (0.75 + rng.Float64()*0.5))

	// Первая категория всегда Standard, остальные — из пула без повторов.
	n := 2 + rng.Intn(3)
	cats := []string{roomPool[0]}
	for _, i := range rng.Perm(len(roomPool) - 1) {
		if len(cats) == n {
			break
		}
		cats = append(cats, roomPool[i+1])
	}

	return domain.Card{
		Mock:           true,
		Stars:          stars,
		PriceEUR:       price,
		Breakfast:      rng.Float64() < 0.3+0.12*float64(stars-2),
		Wifi:           true,
		RoomCategories: cats,
	}
}

func pickStars(r float64) int {
	acc := 0.0
	for i, w := range starWeights {
		acc += w
		if r < acc {
			return i + 2
		}
	}
	return 5
}

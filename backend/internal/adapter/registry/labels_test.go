package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
	embedded "github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

// labelsFile — подписи значений на фронтенде (frontend/src/shared/lib/labels.json). Подписи не лежат в реестре:
// поле в реестре попало бы в list_characteristics агентов и сломало их кеши. Этот тест не даёт новой характеристике
// появиться без подписей.
type labelsFile struct {
	Bool            map[string]string `json:"bool"`
	Characteristics map[string]struct {
		Values  map[string]string `json:"values"`
		Unit    string            `json:"unit"`
		Summary string            `json:"summary"`
	} `json:"characteristics"`
	Contract    map[string]string `json:"contract"`
	BookingWish map[string]string `json:"booking_wish"`
}

func loadLabels(t *testing.T) labelsFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "frontend", "src", "shared", "lib", "labels.json"))
	if err != nil {
		t.Fatalf("подписи фронтенда: %v", err)
	}
	var l labelsFile
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestFrontendLabelsCoverRegistry(t *testing.T) {
	l := loadLabels(t)
	reg, err := Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	if l.Bool["true"] == "" || l.Bool["false"] == "" {
		t.Error("нет подписей bool (да / нет)")
	}
	for _, c := range reg.All() {
		lc, ok := l.Characteristics[c.Key]
		if !ok {
			t.Errorf("%s: характеристики нет в labels.json", c.Key)
			continue
		}
		values := append([]string(nil), c.Values...)
		if c.Category != nil {
			values = append(values, c.Category.Values...)
		}
		for _, v := range values {
			if lc.Values[v] == "" {
				t.Errorf("%s: нет подписи значения %q", c.Key, v)
			}
		}
		switch c.ValueType {
		case domain.ValueNumber:
			if lc.Unit == "" {
				t.Errorf("%s: у числа нет единицы (unit)", c.Key)
			}
		case domain.ValueRange:
			if lc.Unit == "" && lc.Summary == "" {
				t.Errorf("%s: у диапазона нет ни unit, ни summary", c.Key)
			}
		}
	}
}

func TestFrontendLabelsCoverStatuses(t *testing.T) {
	l := loadLabels(t)
	for _, k := range []string{
		domain.ContractGuaranteed, domain.ContractChance, domain.ContractWishPending, domain.ContractWishConfirmed,
		domain.ContractWishRefused, domain.ContractUncertain, domain.ContractUnknown, domain.ContractMismatch,
	} {
		if l.Contract[k] == "" {
			t.Errorf("нет подписи пункта контракта %q", k)
		}
	}
	for _, k := range []string{booking.WishWaiting, booking.WishConfirmed, booking.WishRefused} {
		if l.BookingWish[k] == "" {
			t.Errorf("нет подписи статуса пожелания в брони %q", k)
		}
	}
}

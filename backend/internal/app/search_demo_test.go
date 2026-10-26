package app

import (
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// Запросы проверки поиска проходят схему желаний, выведенную из встроенного реестра.
func TestDemoQueriesMatchRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(search.DemoQueries) != 5 {
		t.Fatalf("запросов проверки %d, ждали 5", len(search.DemoQueries))
	}
	for _, dq := range search.DemoQueries {
		for _, w := range dq.Query.Wishes {
			if _, _, err := domain.NormalizeWish(reg, w); err != nil {
				t.Errorf("%q: %v", dq.Text, err)
			}
		}
	}
}

// wish_guarantee — характеристика только от отельера: без стратегий, с вопросом.
func TestWishGuaranteeInRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reg.Get(domain.WishGuaranteeKey)
	if !ok || len(c.Strategies) != 0 || c.AskHotelier == "" || c.ValueType != domain.ValueBool {
		t.Fatalf("wish_guarantee: %+v (найдена %v)", c, ok)
	}
}

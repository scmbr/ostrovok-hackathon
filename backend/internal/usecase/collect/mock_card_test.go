package collect

import (
	"reflect"
	"testing"
)

func TestMockCardDeterministic(t *testing.T) {
	a, b := MockCard(12345), MockCard(12345)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("не детерминирован: %+v vs %+v", a, b)
	}
}

func TestMockCardInvariants(t *testing.T) {
	for id := int64(-500); id < 500; id++ {
		c := MockCard(id)
		if !c.Mock {
			t.Fatalf("id %d: нет пометки mock", id)
		}
		if c.Stars < 2 || c.Stars > 5 {
			t.Fatalf("id %d: звёзды %d", id, c.Stars)
		}
		if c.PriceEUR < basePrice[c.Stars]*3/4-1 || c.PriceEUR > basePrice[c.Stars]*5/4 {
			t.Fatalf("id %d: цена %d вне диапазона для %d звёзд", id, c.PriceEUR, c.Stars)
		}
		if n := len(c.RoomCategories); n < 2 || n > 4 || c.RoomCategories[0] != "Standard" {
			t.Fatalf("id %d: категории %v", id, c.RoomCategories)
		}
		seen := map[string]bool{}
		for _, r := range c.RoomCategories {
			if seen[r] {
				t.Fatalf("id %d: повтор категории %s", id, r)
			}
			seen[r] = true
		}
	}
}

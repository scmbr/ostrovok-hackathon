package postgres

import (
	"context"
	"testing"
)

const testDemandKey = "test_demand_key"

// Спрос из поиска: сессия увеличивает его по ключу один раз; мок остаётся отдельной строкой, сумма идёт в Demand.
func TestRecordDemandOncePerSession(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM characteristic_demand WHERE characteristic_key = $1`, testDemandKey)
		_, _ = pool.Exec(ctx, `DELETE FROM demand_sessions WHERE characteristic_key = $1`, testDemandKey)
	}
	clean()
	t.Cleanup(clean)
	if _, err := pool.Exec(ctx, `INSERT INTO characteristic_demand (characteristic_key, region_id, count, source) VALUES ($1, 1, 10, 'mock')`, testDemandKey); err != nil {
		t.Skipf("миграция 0019 не применена или нет региона 1: %v", err)
	}
	s := NewSearchStore(pool)
	for _, call := range []struct{ session string }{{"a"}, {"a"}, {"b"}, {""}, {""}} {
		if err := s.Record(ctx, 1, call.session, []string{testDemandKey}); err != nil {
			t.Fatal(err)
		}
	}
	var mock, search int
	if err := pool.QueryRow(ctx, `SELECT count FROM characteristic_demand WHERE characteristic_key = $1 AND source = 'mock'`, testDemandKey).Scan(&mock); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count FROM characteristic_demand WHERE characteristic_key = $1 AND source = $2`, testDemandKey, SearchSource).Scan(&search); err != nil {
		t.Fatal(err)
	}
	// Сессии a (дважды) и b — по одному разу, два запроса без сессии — по разу каждый.
	if mock != 10 || search != 4 {
		t.Errorf("мок %d (ждали 10), поиск %d (ждали 4)", mock, search)
	}
	total, err := (&HotelierStore{pool: pool}).Demand(ctx, 1)
	if err != nil || total[testDemandKey] != 14 {
		t.Errorf("Demand суммирует источники: %v %v", total[testDemandKey], err)
	}
}

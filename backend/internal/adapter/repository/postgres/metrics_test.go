package postgres

import (
	"context"
	"strings"
	"testing"
)

// Запись через хранилище метрик невозможна: транзакция открыта на чтение. Тест идёт по БД (пропускается без неё).
func TestMetricsStoreIsReadOnly(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	st, err := BeginMetrics(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(ctx)

	if _, err := st.DemoHotels(ctx); err != nil {
		t.Fatalf("чтение должно работать: %v", err)
	}
	// UPDATE без строк всё равно запрещён в READ ONLY.
	_, err = st.tx.Exec(ctx, `UPDATE hotels SET name = name WHERE false`)
	if err == nil || !strings.Contains(err.Error(), "read-only transaction") {
		t.Fatalf("запись в транзакции метрик должна отклоняться, получено: %v", err)
	}
}

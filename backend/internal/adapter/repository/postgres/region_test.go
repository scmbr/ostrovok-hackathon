package postgres

import (
	"context"
	"testing"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func TestLoadRegionBarcelona(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegionStore(pool).LoadRegion(ctx, "barcelona")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Districts) != 6 || r.Districts[0].Name != "vila_olimpica" || r.Districts[5].Name != "eixample" {
		t.Errorf("районы в порядке приоритета: %+v", r.Districts)
	}
	// Контур — огибающая районов: ровно тот охват, что раньше считался в коде.
	want := domain.BBox{South: 41.3577, West: 2.160, North: 41.4118, East: 2.2220}
	if r.Contour != want {
		t.Errorf("контур %+v, want %+v", r.Contour, want)
	}
	if got := r.DistrictOf(orb.Point{2.1965, 41.3875}); got != "vila_olimpica" {
		t.Errorf("район точки на пересечении = %q, want vila_olimpica (меньший приоритет)", got)
	}
	if _, err := NewRegionStore(pool).LoadRegion(ctx, "atlantis"); err == nil {
		t.Error("несуществующий регион должен давать ошибку")
	}
}

// Replace* заменяют строки только своего региона: чужие остаются нетронутыми.
func TestReplaceSeaIsScopedByRegion(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var other int64
	if err := pool.QueryRow(ctx, `INSERT INTO regions (code, name) VALUES ('test_region', 'Тест') ON CONFLICT (code) DO UPDATE SET name = 'Тест' RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sea WHERE region_id = $1`, other)
		_, _ = pool.Exec(ctx, `DELETE FROM regions WHERE id = $1`, other)
	}
	t.Cleanup(clean)
	_, _ = pool.Exec(ctx, `DELETE FROM sea WHERE region_id = $1`, other)

	count := func(region int64) int {
		return countRows(t, pool, `SELECT count(*) FROM sea WHERE region_id = $1`, region)
	}
	barcelona := count(1)

	store := NewGeoStore(pool, other)
	lines := []orb.LineString{{{0, 0}, {1, 1}}, {{2, 2}, {3, 3}}}
	for i := 0; i < 2; i++ { // повторная замена не копит строки
		if err := store.ReplaceSea(ctx, lines); err != nil {
			t.Fatal(err)
		}
	}
	if got := count(other); got != 2 {
		t.Errorf("строк чужого региона %d, want 2", got)
	}
	if got := count(1); got != barcelona {
		t.Errorf("береговая линия Барселоны изменилась: было %d, стало %d", barcelona, got)
	}
}

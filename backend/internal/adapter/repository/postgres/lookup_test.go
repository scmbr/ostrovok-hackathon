package postgres

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/lookup"
)

// Поиск по названию на живой БД: id и слово названия (без регистра и диакритики) находят тот же отель.
func TestLookupOnDB(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var id, regionID int64
	var name string
	if err := pool.QueryRow(ctx, `SELECT id, name, region_id FROM hotels WHERE demo ORDER BY id LIMIT 1`).Scan(&id, &name, &regionID); err != nil {
		t.Skipf("нет демо-отелей: %v", err)
	}
	svc := &lookup.Service{Store: NewSearchStore(pool), RegionID: regionID}

	res, err := svc.Lookup(ctx, "  "+strconv.FormatInt(id, 10)+" ")
	if err != nil || len(res.Matches) == 0 || res.Matches[0].ID != id || res.Matches[0].By != lookup.MatchID {
		t.Fatalf("по id %d: %+v, %v", id, res, err)
	}
	res, err = svc.Lookup(ctx, strings.ToUpper(name))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range res.Matches {
		found = found || m.ID == id
	}
	if !found {
		t.Errorf("по названию %q отель %d не найден: %+v", name, id, res)
	}
	if r, err := svc.Lookup(ctx, "0"); err != nil || len(r.Matches) != 0 {
		t.Errorf("id 0 не отель: %+v, %v", r, err)
	}
}

package postgres

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

const (
	testHotelID = int64(9_000_000_001) // нет в hotels: тест не трогает настоящие данные
	testKey     = "test_characteristic"
)

// testPool подключается к БД из DATABASE_URL (по умолчанию локальная из docker compose up db).
// Без БД тест пропускается; в отчёте он должен идти как PASS, а не SKIP.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://tima:tima@localhost:5432/tima?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err == nil {
		err = pool.Ping(ctx)
	}
	if err != nil {
		t.Skipf("БД недоступна (docker compose up -d db): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func cleanTestRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{"facts", "evidence", "conflicts"} {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM `+table+` WHERE characteristic_key = $1`, testKey); err != nil {
			t.Fatal(err)
		}
	}
}

func toolEvidence(value string, reliability float64) domain.Evidence {
	return domain.Evidence{
		EntityType: domain.EntityHotel, EntityID: "9000000001", Key: testKey,
		Claimed: map[string]any{"v": value}, Source: domain.SourceComputed, Method: "test_tool@1",
		AuthorType: domain.AuthorTool, AuthorID: "test_tool@1", Reliability: reliability, ObservedAt: time.Now(),
		Details: map[string]any{},
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Повторная запись результата инструмента снимает только его прежние свидетельства: ответ отельера остаётся активным,
// факт пересобирается из всех активных свидетельств.
func TestSaveEvidenceKeepsHumanEvidence(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanTestRows(t, pool)
	t.Cleanup(func() { cleanTestRows(t, pool) })
	tool := []domain.Author{domain.ToolAuthor("test_tool@1")}
	w := evidenceWriter{pool: pool}

	if err := w.SaveHotelEvidence(ctx, testHotelID, testKey, tool, []domain.Evidence{toolEvidence("first", 0.85)}); err != nil {
		t.Fatal(err)
	}
	hotelier := toolEvidence("from hotelier", 0.8)
	hotelier.AuthorType, hotelier.AuthorID, hotelier.Source = domain.AuthorHotelier, "hotel-9000000001", domain.AuthorHotelier
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insertEvidence(ctx, tx, hotelier); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := w.SaveHotelEvidence(ctx, testHotelID, testKey, tool, []domain.Evidence{toolEvidence("second", 0.85)}); err != nil {
		t.Fatal(err)
	}

	active := func(author string) int {
		return countRows(t, pool, `SELECT count(*) FROM evidence WHERE characteristic_key = $1 AND status = 'active' AND author_type = $2`, testKey, author)
	}
	if n := active(domain.AuthorHotelier); n != 1 {
		t.Errorf("активных свидетельств отельера %d, want 1: инструмент не должен их снимать", n)
	}
	if n := active(domain.AuthorTool); n != 1 {
		t.Errorf("активных свидетельств инструмента %d, want 1", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM evidence WHERE characteristic_key = $1 AND status = 'superseded' AND author_type = 'tool'`, testKey); n != 1 {
		t.Errorf("снятых свидетельств инструмента %d, want 1", n)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM facts WHERE characteristic_key = $1 AND status = 'active'`, testKey); n != 1 {
		t.Fatalf("активных фактов %d, want 1", n)
	}
	var value string
	var ids []int64
	if err := pool.QueryRow(ctx, `SELECT value->>'v', evidence_ids FROM facts WHERE characteristic_key = $1 AND status = 'active'`, testKey).Scan(&value, &ids); err != nil {
		t.Fatal(err)
	}
	if value != "second" || len(ids) != 2 {
		t.Errorf("факт = %q по свидетельствам %v, want second (сильнейшее, 0.85 > 0.8) по двум свидетельствам", value, ids)
	}

	// Вклады пишет движок правды: победившее свидетельство инструмента — ln(0.85/0.15), слабейший ответ отельера — 0.
	var toolContribution, hotelierContribution float64
	for author, dst := range map[string]*float64{domain.AuthorTool: &toolContribution, domain.AuthorHotelier: &hotelierContribution} {
		if err := pool.QueryRow(ctx, `SELECT contribution FROM evidence WHERE characteristic_key = $1 AND status = 'active' AND author_type = $2`,
			testKey, author).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(toolContribution-math.Log(0.85/0.15)) > 1e-3 || hotelierContribution != 0 {
		t.Errorf("вклады: инструмент %.4f, отельер %.4f", toolContribution, hotelierContribution)
	}

	// Пустой результат инструмента (например, маршрут исчез) снимает его свидетельство; факт собирается из ответа отельера.
	if err := w.SaveHotelEvidence(ctx, testHotelID, testKey, tool, nil); err != nil {
		t.Fatal(err)
	}
	if n := active(domain.AuthorTool); n != 0 {
		t.Errorf("активных свидетельств инструмента %d, want 0", n)
	}
	if err := pool.QueryRow(ctx, `SELECT value->>'v' FROM facts WHERE characteristic_key = $1 AND status = 'active'`, testKey).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "from hotelier" {
		t.Errorf("факт = %q, want ответ отельера", value)
	}
}

// Запись свидетельств отеля вызывает реакцию с отелем и характеристикой; ошибка реакции возвращается вызывающему.
func TestSaveHotelEvidenceCallsHook(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanTestRows(t, pool)
	t.Cleanup(func() { cleanTestRows(t, pool) })
	w := &evidenceWriter{pool: pool}
	tool := []domain.Author{domain.ToolAuthor("test_tool@1")}

	var gotHotel int64
	var gotKey string
	w.SetAfterHotelSave(func(_ context.Context, hotelID int64, key string) error { gotHotel, gotKey = hotelID, key; return nil })
	if err := w.SaveHotelEvidence(ctx, testHotelID, testKey, tool, []domain.Evidence{toolEvidence("x", 0.85)}); err != nil {
		t.Fatal(err)
	}
	if gotHotel != testHotelID || gotKey != testKey {
		t.Errorf("реакция получила отель %d, ключ %q", gotHotel, gotKey)
	}
	w.SetAfterHotelSave(func(context.Context, int64, string) error { return errors.New("проекция упала") })
	if err := w.SaveHotelEvidence(ctx, testHotelID, testKey, tool, nil); err == nil {
		t.Error("ошибка реакции должна вернуться вызывающему")
	}
}

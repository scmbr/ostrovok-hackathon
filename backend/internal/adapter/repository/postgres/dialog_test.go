package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

const testSessionPrefix = "test-dialog-"

func TestDialogSessionTTLAndCleanup(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM dialog_sessions WHERE session_id LIKE $1`, testSessionPrefix+"%")
	}
	clean()
	t.Cleanup(clean)
	s := NewDialogStore(pool, time.Hour)
	if err := s.SaveState(ctx, testSessionPrefix+"a", []byte(`{"n":1}`)); err != nil {
		t.Skipf("миграция 0020 не применена: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO dialog_sessions (session_id, state, updated_at) VALUES ($1, '{}', now() - interval '2 hours')`, testSessionPrefix+"old"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LoadState(ctx, testSessionPrefix+"old"); err != nil || ok {
		t.Errorf("сессия старше часа не читается: ok=%v err=%v", ok, err)
	}
	var left int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM dialog_sessions WHERE session_id = $1`, testSessionPrefix+"old").Scan(&left)
	if left != 0 {
		t.Error("устаревшая строка не удалена при чтении")
	}
	got, ok, err := s.LoadState(ctx, testSessionPrefix+"a")
	if err != nil || !ok || string(got) != `{"n": 1}` {
		t.Errorf("свежая сессия читается: %s ok=%v err=%v", got, ok, err)
	}
}

func TestLogUnmetCountsRepeats(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	clean := func() { _, _ = pool.Exec(ctx, `DELETE FROM unmet_wishes WHERE normalized = 'тест желание'`) }
	clean()
	t.Cleanup(clean)
	s := NewDialogStore(pool, time.Hour)
	var last int
	for i := 0; i < 3; i++ {
		n, err := s.LogUnmet(ctx, 1, "Тест-желание!", "тест желание", nil)
		if err != nil {
			t.Skipf("миграция 0020 не применена: %v", err)
		}
		last = n
	}
	if last != 3 {
		t.Errorf("счётчик повторов %d, ждали 3", last)
	}
	if n, _ := s.LogUnmet(ctx, 1, "тест желание", "тест желание", []byte(`{"key":"x"}`)); n != 4 {
		t.Errorf("счётчик %d, ждали 4", n)
	}
	var proposal []byte
	_ = pool.QueryRow(ctx, `SELECT proposal FROM unmet_wishes WHERE normalized = 'тест желание'`).Scan(&proposal)
	if string(proposal) != `{"key": "x"}` {
		t.Errorf("предложение записано: %s", proposal)
	}
}

// Журнал диалога не привязан к отелю: hotel_id = 0 пишется как NULL, а не нарушает внешний ключ.
func TestAppendTraceWithoutHotel(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	const sid = "test-trace-no-hotel"
	_, _ = pool.Exec(ctx, `DELETE FROM agent_trace WHERE session_id = $1`, sid)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM agent_trace WHERE session_id = $1`, sid) })
	err := NewAgentStore(pool, nil).AppendTrace(ctx, agent.TraceStep{Agent: "guest_dialog", SessionID: sid, Mode: "rule", Step: 1, Decision: "тест"})
	if err != nil {
		t.Fatal(err)
	}
	var isNull bool
	if err := pool.QueryRow(ctx, `SELECT hotel_id IS NULL FROM agent_trace WHERE session_id = $1`, sid).Scan(&isNull); err != nil || !isNull {
		t.Errorf("hotel_id должен быть NULL: %v %v", isNull, err)
	}
}

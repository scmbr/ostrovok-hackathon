package app

import (
	"context"
	"os"
	"testing"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
)

// demoAgentHotels — отели, прогоны агента по которым записаны в кеш (adapter/llm/cache/agent).
var demoAgentHotels = []int64{161, 177, 188, 279, 343}

// Реплей кеша агента по пяти демо-отелям на базе, загруженной из демо-сида, не даёт промахов: режим прогона остаётся
// cache до конца, планировщик не подключается. Промах значит, что вывод get_hotel_facts (или другого инструмента)
// разошёлся с записанным и кеш пора перезаписывать.
//
// Агент запускает расчёты и пишет журнал, поэтому тест идёт только на отдельной базе проверки с нуля:
//
//	docker compose -p clean-check up --build -d   (рабочий стек предварительно: docker compose stop)
//	AGENT_REPLAY_DATABASE_URL=postgres://tima:tima@localhost:5432/tima?sslmode=disable go test ./internal/app -run AgentReplay
//
// Без переменной тест пропускается: рабочая БД содержит данные, которых нет в сиде (граф дорог, части зданий).
func TestAgentReplayFromSeedHasNoMisses(t *testing.T) {
	url := os.Getenv("AGENT_REPLAY_DATABASE_URL")
	if url == "" {
		t.Skip("AGENT_REPLAY_DATABASE_URL не задан: нужна база из демо-сида (docker compose -p clean-check)")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	region, err := postgres.NewRegionStore(pool).LoadRegion(ctx, defaultRegion)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := adapterllm.NewAgentReplay()
	if err != nil {
		t.Fatal(err)
	}
	rules := reg.Rules()
	store := postgres.NewAgentStore(pool, rules)
	runner := newToolRunner(reg, rules, pool, region, t.TempDir())
	runner.check = store.RequireSources

	for _, id := range demoAgentHotels {
		a := &agent.Agent{Registry: reg, Store: store, Runner: runner, Chat: replay, ChatMode: agent.ModeCache}
		res, err := a.Run(ctx, id)
		if err != nil {
			t.Fatalf("отель %d: %v", id, err)
		}
		steps, err := store.ListTrace(ctx, id, res.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		misses := 0
		for _, s := range steps {
			if s.Mode != agent.ModeCache {
				misses++
			}
		}
		if res.Mode != agent.ModeCache || misses > 0 {
			t.Errorf("отель %d: режим в конце %s, шагов не из кеша %d из %d — промах кеша", id, res.Mode, misses, len(steps))
		}
	}
}

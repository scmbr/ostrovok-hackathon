package app

import (
	"context"
	"os"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// Реплей демо-диалогов (demo_dialogs.json) из кеша без ключа на базе из демо-сида не даёт промахов: ни один ход не
// переходит на правила (шаг fallback_rules), а модель отвечает из кеша. Промах значит, что подсказка, инструменты или
// их результаты разошлись с записанными и кеш диалога пора перезаписывать (`collect dialog -fixture имя -record имя`).
//
// Тест идёт на отдельной базе проверки с нуля (make test-replay-dialog); без переменной он пропускается.
func TestDialogReplayFromSeedHasNoMisses(t *testing.T) {
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
	roomSvc := &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, reg.Rules())}
	searchStore := postgres.NewSearchStore(pool)
	searchSvc := &search.Service{Registry: reg, Store: searchStore, Rooms: roomSvc, Demand: searchStore, RegionID: region.ID}
	// dialogCache: ключ не используется, даже если он есть в окружении.
	e, _, _, err := newDialogEngineFrom(Config{}, reg, pool, region, searchSvc, dialogCache, false)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := dialog.DemoDialogs()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		session := "replay-" + d.Name
		if err := e.Sessions.SaveState(ctx, session, []byte("{}")); err != nil {
			t.Fatal(err)
		}
		cacheSteps, misses, turns := 0, 0, 0
		for i, text := range d.Turns {
			req := dialog.Request{SessionID: session, Message: text}
			if len(text) > 0 && text[0] == '@' {
				req.Message, req.Choice = "", text[1:]
			}
			err := e.Turn(ctx, req, func(ev dialog.Event) {
				s, ok := ev.Data.(dialog.StepEvent)
				if !ok {
					return
				}
				if s.Mode == dialog.ModeCache {
					cacheSteps++
				}
				if s.Tool == "fallback_rules" {
					misses++
					t.Errorf("%s, ход %d (%q): промах кеша: %s", d.Name, i+1, text, s.Decision)
				}
			})
			if err != nil {
				t.Fatalf("%s, ход %d: %v", d.Name, i+1, err)
			}
			turns++
		}
		if cacheSteps == 0 {
			t.Errorf("%s: ни одного шага из кеша — диалог не записан", d.Name)
		}
		t.Logf("%s: ходов %d, шагов из кеша %d, промахов %d", d.Name, turns, cacheSteps, misses)
	}
}

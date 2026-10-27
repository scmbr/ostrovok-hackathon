// Package app собирает зависимости и запускает приложение.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	apphttp "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/http"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/admin"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/explain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// Run запускает HTTP-сервер и блокируется до SIGINT/SIGTERM.
func Run() error {
	loadDotEnv()
	cfg := LoadConfig()

	reg, err := LoadRegistry()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("миграции: %w", err)
	}
	if _, err := postgres.LoadSeed(ctx, pool, cfg.SeedFile); err != nil {
		return fmt.Errorf("сид: %w", err)
	}

	if cfg.LLMAPIKey == "" {
		log.Println("LLM_API_KEY не задан: режим кеша (без LLM)")
	}

	cabinet, err := newServerInterviewer(cfg, reg, pool)
	if err != nil {
		return fmt.Errorf("кабинет отельера: %w", err)
	}
	region, err := postgres.NewRegionStore(pool).LoadRegion(ctx, defaultRegion)
	if err != nil {
		return fmt.Errorf("регион поиска: %w", err)
	}
	roomSvc := &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, reg.Rules())}
	searchStore := postgres.NewSearchStore(pool)
	searchSvc := &search.Service{Registry: reg, Store: searchStore, Rooms: roomSvc, Demand: searchStore, RegionID: region.ID}
	chat, _, mode, err := newDialogEngineFrom(cfg, reg, pool, region, searchSvc, dialogAuto, false)
	if err != nil {
		return fmt.Errorf("агент диалога: %w", err)
	}
	log.Println("диалог: " + mode)
	examples, err := dialog.Examples()
	if err != nil {
		return fmt.Errorf("демо-диалоги: %w", err)
	}
	guest := &booking.Service{Registry: reg, Search: searchSvc, Sessions: postgres.NewDialogStore(pool, dialog.SessionTTL), Hotelier: cabinet,
		Demand: searchStore, Bookings: postgres.NewBookingStore(pool), Trace: postgres.NewAgentStore(pool, reg.Rules()), RegionID: region.ID}
	chat.Compare = guest
	explainSvc := &explain.Service{Registry: reg, Store: postgres.NewExplainStore(pool)}
	adminSvc := &admin.Service{Registry: reg, Store: postgres.NewAdminStore(pool), Explain: explainSvc}
	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: apphttp.NewRouter(reg, postgres.NewAgentStore(pool, reg.Rules()),
			roomSvc, cabinet, apphttp.WithSearch(searchSvc), apphttp.WithChat(chat), apphttp.WithGuestFlow(guest), apphttp.WithDialogExamples(examples), apphttp.WithLLMMode(llmMode(cfg), cfg.LLMModel), apphttp.WithAdmin(adminSvc, explainSvc)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("слушаю :%s", cfg.Port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// llmMode — режим LLM для /api/meta: есть ключ — живая модель, нет — сохранённые ответы.
func llmMode(cfg Config) string {
	if cfg.LLMAPIKey != "" {
		return apphttp.LLMModeLive
	}
	return apphttp.LLMModeCache
}

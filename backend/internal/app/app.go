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

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           apphttp.NewRouter(reg, postgres.NewAgentStore(pool, reg.Rules())),
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

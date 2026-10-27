package app

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/reputation"
)

// runReputationRecompute — collect reputation-recompute: репутация всех отельеров заново по активным свидетельствам
// (reputation.Service.Update), затем пересборка фактов их сущностей. Нужна, чтобы проверить правило судьи на сиде:
// отпечатки author_trust, фактов, свидетельств и конфликтов до и после правила сравниваются.
func runReputationRecompute(args []string, reg domain.Registry) error {
	if len(args) > 0 {
		return errors.New("reputation-recompute не принимает аргументов")
	}
	cfg := LoadConfig()
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
	store := postgres.NewReputationStore(pool, reg.Rules())
	svc := &reputation.Service{Registry: reg, Store: store}
	authors, err := store.Authors(ctx, domain.AuthorHotelier)
	if err != nil {
		return err
	}
	changed := 0
	for _, a := range authors {
		up, err := svc.Update(ctx, a)
		if err != nil {
			return fmt.Errorf("отельер %s: %w", a.ID, err)
		}
		mark := ""
		if up.Before != up.After {
			changed++
			mark = "  <- изменилась"
		}
		fmt.Printf("отельер %s: %.4f -> %.4f (совпало %d из %d)%s\n", a.ID, up.Before, up.After, up.Agree, up.Total, mark)
	}
	fmt.Printf("отельеров %d, репутация изменилась у %d\n", len(authors), changed)
	return nil
}

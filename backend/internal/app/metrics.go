package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/labels"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/metrics"
)

// Границы блока с таблицами в docs/metrics.md: команда заменяет только то, что между ними.
const (
	metricsBegin = "<!-- metrics:begin -->"
	metricsEnd   = "<!-- metrics:end -->"
)

// runMetrics — collect metrics [-labels каталог] [-write файл]: метрики качества на разметке data/labels и сиде.
// Только чтение БД (миграции не применяются, ничего не пишется); таблицы печатаются в stdout, с -write ещё и вставляются
// между маркерами в docs/metrics.md.
func runMetrics(args []string, reg domain.Registry) error {
	fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
	dir := fs.String("labels", "../data/labels", "каталог с файлами разметки *.yaml")
	write := fs.String("write", "", "файл Markdown, в котором заменить блок между "+metricsBegin+" и "+metricsEnd)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ls, err := labels.LoadDir(*dir)
	if err != nil {
		return fmt.Errorf("разметка: %w", err)
	}

	cfg := LoadConfig()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer pool.Close()

	var chars []metrics.CharInfo
	for _, c := range reg.All() {
		chars = append(chars, metrics.CharInfo{Key: c.Key, Title: c.Title, Level: c.Level, AskHotelier: c.AskHotelier != ""})
	}
	in, err := metrics.Load(ctx, postgres.NewMetricsStore(pool), ls, chars)
	if err != nil {
		return err
	}
	out := metrics.Render(metrics.Compute(in))
	fmt.Print(out)
	if *write == "" {
		return nil
	}
	return replaceBlock(*write, out)
}

func replaceBlock(path, body string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(raw)
	i, j := strings.Index(s, metricsBegin), strings.Index(s, metricsEnd)
	if i < 0 || j < i {
		return fmt.Errorf("в %s нет маркеров %s … %s", path, metricsBegin, metricsEnd)
	}
	next := s[:i+len(metricsBegin)] + "\n\n" + strings.TrimRight(body, "\n") + "\n\n" + s[j:]
	return os.WriteFile(path, []byte(next), 0o644)
}

package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
	usecaseregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

// newLLM выбирает клиента: с ключом — OpenAI-совместимое API, без ключа — режим кеша (заготовленные ответы).
func newLLM(cfg Config) (usecaseregistry.LLM, bool, error) {
	if cfg.LLMAPIKey != "" {
		return adapterllm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel), true, nil
	}
	cached, err := adapterllm.NewCached()
	return cached, false, err
}

// runAddCharacteristic: collect add-characteristic [-dry-run] [-dir registry] "<описание>".
// Новая характеристика записывается файлом реестра; сервер подхватит её после пересборки.
func runAddCharacteristic(args []string, reg domain.Registry) error {
	fs := flag.NewFlagSet("add-characteristic", flag.ContinueOnError)
	dir := fs.String("dir", "registry", "каталог YAML реестра (запуск из backend/)")
	dry := fs.Bool("dry-run", false, "только показать YAML, файл не записывать")
	if err := fs.Parse(args); err != nil {
		return err
	}
	description := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if description == "" {
		return errors.New(`укажите описание: collect add-characteristic "можно ли с собакой"`)
	}

	cfg := LoadConfig()
	client, live, err := newLLM(cfg)
	if err != nil {
		return fmt.Errorf("LLM: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if live {
		fmt.Printf("LLM: %s (%s)\n", cfg.LLMModel, cfg.LLMBaseURL)
	} else {
		fmt.Println("LLM_API_KEY не задан: режим кеша (заготовленные ответы)")
	}
	adder := &usecaseregistry.Adder{LLM: client, Registry: reg, Tools: compute.Specs, Parse: adapterregistry.Parse}
	res, err := adder.Add(ctx, description)
	if err != nil {
		return err
	}
	c := res.Characteristic
	body, err := adapterregistry.Marshal(c)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("# Добавлено командой collect add-characteristic: %q.\n# LLM описала характеристику, проверка реестра пройдена; значения фактов LLM не пишет.\n", description)
	text := header + string(body)

	for _, n := range res.Notes {
		fmt.Println("правка:", n)
	}
	if *dry {
		fmt.Printf("\n%s", text)
		return nil
	}
	path := filepath.Join(*dir, c.Key+".yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s уже существует", path)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Printf("записано: %s (попыток LLM: %d)\n\n%s\n", path, res.Attempts, text)
	fmt.Println(nextSteps(c))
	return nil
}

// nextSteps подсказывает, что делать дальше: пересчитать инструментами или ждать ответа отельера.
func nextSteps(c domain.Characteristic) string {
	if len(c.Strategies) == 0 {
		return "Инструментов для неё нет: значение появится из ответа отельера (стратегия ask_hotelier, этап 5)."
	}
	var cmds []string
	seen := map[string]bool{}
	for _, s := range c.Strategies {
		if !seen[s.Tool] {
			seen[s.Tool] = true
			cmds = append(cmds, "go run ./cmd/collect "+s.Tool)
		}
	}
	return "Дальше: пересоберите сервер (реестр встроен в бинарник) и посчитайте значения: " + strings.Join(cmds, ", ") + "."
}

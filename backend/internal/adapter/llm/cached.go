package llm

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

//go:embed cache/*.yaml
var builtinCache embed.FS

// entry — сохранённый ответ LLM: на какую задачу и на какие формулировки он отвечает.
type entry struct {
	Task     string   `yaml:"task"`
	Inputs   []string `yaml:"inputs"`
	Response string   `yaml:"response"`
}

// ErrNotCached — в режиме кеша нет ответа на этот вход. Examples — входы, на которые ответ есть.
type ErrNotCached struct {
	Task     string
	Examples []string
}

func (e *ErrNotCached) Error() string {
	return fmt.Sprintf("в режиме кеша (LLM_API_KEY не задан) нет ответа на запрос; задайте LLM_API_KEY или используйте одну из заготовок: %s",
		strings.Join(e.Examples, "; "))
}

// Cached — режим без LLM: отвечает сохранёнными ответами. Вход сравнивается по нормализованному тексту.
type Cached struct {
	entries []entry
}

// NewCached читает встроенные заготовки (cache/*.yaml).
func NewCached() (*Cached, error) { return newCached(builtinCache, "cache") }

func newCached(fsys fs.FS, dir string) (*Cached, error) {
	names, err := fs.Glob(fsys, dir+"/*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	c := &Cached{}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var e entry
		if err := yaml.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if e.Task == "" || len(e.Inputs) == 0 || strings.TrimSpace(e.Response) == "" {
			return nil, fmt.Errorf("%s: нужны task, inputs и response", name)
		}
		c.entries = append(c.entries, e)
	}
	return c, nil
}

// Complete возвращает ответ, если нормализованный вход совпал с одной из формулировок заготовки или содержит её.
func (c *Cached) Complete(_ context.Context, req registry.Request) (string, error) {
	var examples []string
	for _, e := range c.entries {
		if e.Task != req.Task {
			continue
		}
		examples = append(examples, fmt.Sprintf("«%s»", e.Inputs[0]))
		for _, in := range e.Inputs {
			if n := registry.NormalizeInput(in); n != "" && strings.Contains(" "+req.Key+" ", " "+n+" ") {
				return e.Response, nil
			}
		}
	}
	return "", &ErrNotCached{Task: req.Task, Examples: examples}
}

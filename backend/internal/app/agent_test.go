package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

func TestToolRunnerSkipsToolWithoutSources(t *testing.T) {
	ran := false
	r := &toolRunner{
		check: func(_ context.Context, tool string) error { return errors.New("нет данных для " + tool) },
		run: map[string]func(context.Context, compute.Options) (string, error){
			"walk": func(context.Context, compute.Options) (string, error) { ran = true; return "ok", nil },
		},
	}
	if _, err := r.Run(context.Background(), agent.HotelInfo{ID: 1}, "walk", nil); err == nil {
		t.Fatal("ожидалась ошибка проверки исходных данных")
	}
	if ran {
		t.Error("инструмент запущен, хотя исходных данных нет: факты были бы тронуты")
	}
	if _, err := r.Run(context.Background(), agent.HotelInfo{ID: 1}, "unknown", nil); err == nil {
		t.Error("неизвестный инструмент должен давать ошибку")
	}
}

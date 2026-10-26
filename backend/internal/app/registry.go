package app

import (
	"fmt"

	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
	usecaseregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

// LoadRegistry читает встроенный реестр характеристик и проверяет его против инструментов.
// Ошибка реестра останавливает запуск: приложение не должно работать с неверными правилами.
func LoadRegistry() (domain.Registry, error) {
	reg, err := adapterregistry.Load(registry.FS)
	if err != nil {
		return domain.Registry{}, fmt.Errorf("реестр характеристик: %w", err)
	}
	if err := usecaseregistry.Validate(reg, compute.Specs); err != nil {
		return domain.Registry{}, fmt.Errorf("реестр характеристик: %w", err)
	}
	return reg, nil
}

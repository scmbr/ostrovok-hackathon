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
	poi, err := adapterregistry.LoadPoi(registry.FS)
	if err != nil {
		return domain.Registry{}, fmt.Errorf("словарь заведений: %w", err)
	}
	reg = reg.WithPoi(poi)
	if err := usecaseregistry.Validate(reg, compute.Specs); err != nil {
		return domain.Registry{}, fmt.Errorf("реестр характеристик: %w", err)
	}
	return reg, nil
}

// LoadRegistryWith — встроенный реестр плюс характеристики, добавленные текстом (custom_characteristics). Общая проверка та
// же. Ключ, совпавший со встроенным или добавленным раньше, — ошибка: тихой перезаписи нет.
func LoadRegistryWith(rows []usecaseregistry.CustomRow) (domain.Registry, error) {
	base, err := LoadRegistry()
	if err != nil {
		return domain.Registry{}, err
	}
	if len(rows) == 0 {
		return base, nil
	}
	cs := base.All()
	for _, r := range rows {
		c, err := adapterregistry.Parse([]byte(r.YAML))
		if err != nil {
			return domain.Registry{}, fmt.Errorf("добавленная характеристика %s: %w", r.Key, err)
		}
		if c.Key != r.Key {
			return domain.Registry{}, fmt.Errorf("добавленная характеристика: ключ в таблице %q, в YAML %q", r.Key, c.Key)
		}
		cs = append(cs, c)
	}
	reg, err := domain.NewRegistry(cs)
	if err != nil {
		return domain.Registry{}, fmt.Errorf("реестр характеристик: %w", err)
	}
	reg = reg.WithPoi(base.Poi())
	if err := usecaseregistry.Validate(reg, compute.Specs); err != nil {
		return domain.Registry{}, fmt.Errorf("реестр характеристик: %w", err)
	}
	return reg, nil
}

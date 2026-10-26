package app

import (
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Встроенный реестр проходит проверку против настоящих инструментов: иначе сервер и collect не запустятся.
func TestEmbeddedRegistryIsValid(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{domain.SeaViewKey, domain.NoiseNightKey, domain.WalkToBeachKey, domain.WalkToMetroKey,
		domain.NightServicesKey, domain.ClimateKey} {
		if _, ok := reg.Get(key); !ok {
			t.Errorf("в реестре нет %s", key)
		}
	}
}

package compute

import (
	"fmt"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// StrategyCfg — что инструмент берёт из реестра для одной характеристики и стратегии: срок годности,
// базу надёжности, группу независимости и параметры.
type StrategyCfg struct {
	Key         string
	TTL         time.Duration
	Reliability float64
	Group       string
	Strategy    domain.Strategy
}

// strategyCfg находит стратегию характеристики key для инструмента tool и варианта variant.
func strategyCfg(reg domain.Registry, key, tool, variant string) (StrategyCfg, error) {
	c, ok := reg.Get(key)
	if !ok {
		return StrategyCfg{}, fmt.Errorf("характеристика %s не описана в реестре", key)
	}
	s, ok := reg.StrategyFor(key, tool, variant)
	if !ok {
		return StrategyCfg{}, fmt.Errorf("у характеристики %s нет стратегии с инструментом %s (вариант %q)", key, tool, variant)
	}
	return StrategyCfg{Key: key, TTL: c.TTL(), Reliability: s.BaseReliability, Group: s.IndependenceGroup, Strategy: s}, nil
}

// Until — конец срока годности свидетельства, полученного в момент now.
func (c StrategyCfg) Until(now time.Time) time.Time { return now.Add(c.TTL) }

// WithGroup дописывает группу независимости в детали свидетельства.
func (c StrategyCfg) WithGroup(details map[string]any) map[string]any {
	if c.Group != "" {
		details["independence_group"] = c.Group
	}
	return details
}

// registryParams собирает числовые параметры стратегий инструмента из реестра. Для универсального
// инструмента (AnyKey) параметры у каждой характеристики свои, их разбирает сам инструмент.
func registryParams(reg domain.Registry, tool string) Params {
	out := Params{}
	for _, u := range reg.ToolUses(tool) {
		for _, s := range u.Strategies {
			for k, v := range s.Params {
				out[k] = v
			}
		}
	}
	return out
}

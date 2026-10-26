package compute

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Область запуска инструмента: отдельный отель (и его фасады) или район.
const (
	ScopeHotel    = "hotel"
	ScopeDistrict = "district"
)

// Param — параметр инструмента: числовой, с описанием и значением по умолчанию.
type Param struct {
	Name        string
	Unit        string
	Description string
	Default     float64
}

// Spec — описание инструмента: что считает, к чему применяется и какие параметры принимает.
// Агент-сборщик (этап 4) выбирает инструмент по этому описанию.
type Spec struct {
	Name        string // имя CLI-команды
	Description string
	Keys        []string // характеристики, свидетельства которых пишет инструмент
	Scope       string
	Params      []Param
}

// Params — значения параметров запуска по имени.
type Params map[string]float64

// Resolve подставляет значения по умолчанию. Параметр, которого у инструмента нет, — ошибка.
func (s Spec) Resolve(in Params) (Params, error) {
	out := make(Params, len(s.Params))
	for _, p := range s.Params {
		out[p.Name] = p.Default
	}
	var unknown []string
	for k, v := range in {
		if _, ok := out[k]; !ok {
			unknown = append(unknown, k)
			continue
		}
		out[k] = v
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("инструмент %s: неизвестные параметры %s (есть: %s)", s.Name, strings.Join(unknown, ", "), s.paramNames())
	}
	return out, nil
}

func (s Spec) paramNames() string {
	names := make([]string, len(s.Params))
	for i, p := range s.Params {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// Options — запуск инструмента: область и параметры. Нулевые поля области означают «все».
type Options struct {
	Filter   domain.HotelFilter // для инструментов по отелям
	District string             // для инструментов по районам
	Params   Params
}

// Specs — все инструменты, пишущие свидетельства.
var Specs = []Spec{SeaViewSpec, NoiseNightSpec, WalkSpec, NightServicesSpec, ClimateSpec}

// Пакет registry проверяет реестр характеристик и добавляет новые характеристики по текстовому описанию.
package registry

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

var keyPattern = regexp.MustCompile("^[a-z][a-z0-9_]{1,47}$")

var (
	levels     = []string{domain.LevelHotel, domain.LevelFacade, domain.LevelDistrict}
	valueTypes = []string{domain.ValueBool, domain.ValueEnum, domain.ValueNumber, domain.ValueRange, domain.ValueText}
)

// Validate проверяет реестр против инструментов: ключи, типы значений, правила конфликтов, существование
// инструментов и корректность их параметров. Возвращает все найденные ошибки сразу.
func Validate(reg domain.Registry, tools []compute.Spec) error {
	var errs []error
	for _, c := range reg.All() {
		for _, msg := range validateOne(c, tools) {
			errs = append(errs, fmt.Errorf("%s: %s", c.Key, msg))
		}
	}
	errs = append(errs, checkSharedParams(reg, tools)...)
	return errors.Join(errs...)
}

// ValidateOne проверяет одну характеристику; ключ не должен совпадать с уже существующим в reg.
func ValidateOne(reg domain.Registry, c domain.Characteristic, tools []compute.Spec) error {
	var errs []error
	if _, exists := reg.Get(c.Key); exists {
		errs = append(errs, fmt.Errorf("%s: характеристика уже есть в реестре", c.Key))
	}
	for _, msg := range validateOne(c, tools) {
		errs = append(errs, fmt.Errorf("%s: %s", c.Key, msg))
	}
	return errors.Join(errs...)
}

func validateOne(c domain.Characteristic, tools []compute.Spec) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	if !keyPattern.MatchString(c.Key) {
		add("key должен быть в snake_case латиницей (a-z, 0-9, _), получено %q", c.Key)
	}
	if strings.TrimSpace(c.Title) == "" {
		add("пустой title")
	}
	if strings.TrimSpace(c.Description) == "" {
		add("пустой description")
	}
	if !slices.Contains(levels, c.Level) {
		add("level %q: допустимо %s", c.Level, strings.Join(levels, ", "))
	}
	if !slices.Contains(valueTypes, c.ValueType) {
		add("value_type %q: допустимо %s", c.ValueType, strings.Join(valueTypes, ", "))
	}
	if c.TTLDays <= 0 {
		add("ttl_days должен быть больше нуля")
	}
	out = append(out, validateOrigin(c)...)
	out = append(out, validateValues(c)...)
	out = append(out, validateCategory(c)...)
	out = append(out, validateStrategies(c, tools)...)
	return out
}

func validateOrigin(c domain.Characteristic) []string {
	switch c.Origin {
	case "":
		if c.OriginText != "" || c.AddedAt != "" {
			return []string{"origin_text и added_at задаются только вместе с origin: text"}
		}
	case domain.OriginText:
		var out []string
		if strings.TrimSpace(c.OriginText) == "" {
			out = append(out, "у origin: text нужна исходная фраза origin_text")
		}
		if _, err := time.Parse(time.DateOnly, c.AddedAt); err != nil {
			out = append(out, fmt.Sprintf("added_at должна быть датой YYYY-MM-DD, получено %q", c.AddedAt))
		}
		return out
	default:
		return []string{fmt.Sprintf("origin %q: допустимо text или пусто", c.Origin)}
	}
	return nil
}

func validateValues(c domain.Characteristic) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	allowed := c.Values
	switch c.ValueType {
	case domain.ValueEnum:
		if len(c.Values) < 2 {
			add("у enum нужно не меньше двух values")
		}
		seen := map[string]bool{}
		for _, v := range c.Values {
			if seen[v] {
				add("значение %q повторяется в values", v)
			}
			seen[v] = true
		}
	case domain.ValueBool:
		if len(c.Values) > 0 {
			add("у bool values не задаются (true, false)")
		}
		allowed = []string{"true", "false"}
	default:
		if len(c.Values) > 0 {
			add("values допустимы только у enum")
		}
		allowed = nil
		if len(c.FavorableValues) > 0 {
			add("favorable_values допустимы только у enum и bool")
		}
	}
	if c.Ordered && c.ValueType != domain.ValueEnum {
		add("ordered допустим только у enum")
	}
	for _, v := range c.FavorableValues {
		if allowed != nil && !slices.Contains(allowed, v) {
			add("favorable_values: %q нет среди значений", v)
		}
	}

	if c.ValueType == domain.ValueNumber {
		if c.SignificantDiff <= 0 {
			add("у number significant_diff должен быть больше нуля")
		}
	} else if c.SignificantDiff != 0 {
		add("significant_diff задаётся только у number")
	}

	switch {
	case c.ConflictSteps < 0:
		add("conflict_steps не может быть отрицательным")
	case c.ConflictSteps > 1 && !(c.ValueType == domain.ValueEnum && c.Ordered):
		add("conflict_steps больше 1 имеет смысл только у ordered enum")
	case c.ConflictSteps > 1 && c.ConflictSteps > len(c.Values)-1:
		add("conflict_steps %d больше числа ступеней %d", c.ConflictSteps, len(c.Values)-1)
	}
	if c.ConflictTheta < 0 {
		add("conflict_theta не может быть отрицательным")
	}
	for _, e := range c.ConflictEntities {
		if !slices.Contains(levels, e) {
			add("conflict_entities: %q, допустимо %s", e, strings.Join(levels, ", "))
		}
	}
	return out
}

// validateCategory проверяет правило переноса на категорию номеров (category:).
func validateCategory(c domain.Characteristic) []string {
	k := c.Category
	if k == nil {
		return nil
	}
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf("category: "+format, args...)) }

	if c.Level != domain.LevelFacade {
		add("переносится на категорию только характеристика уровня facade, а level %q", c.Level)
	}
	if len(k.Values) < 2 {
		add("нужно не меньше двух values")
	}
	if strings.TrimSpace(k.ValueField) == "" {
		add("пустой value_field")
	}
	if !slices.Contains(k.Values, k.Target) {
		add("target %q нет среди values", k.Target)
	}
	for _, v := range k.FavorableValues {
		if !slices.Contains(k.Values, v) {
			add("favorable_values: %q нет среди values", v)
		}
	}
	if k.FloorField == "" && !slices.Equal(k.Values, c.Values) {
		add("без floor_field значение берётся из факта фасада, поэтому values должны совпадать со значениями характеристики %v", c.Values)
	}
	switch {
	case k.ConflictSteps < 0:
		add("conflict_steps не может быть отрицательным")
	case k.ConflictSteps > 1 && !k.Ordered:
		add("conflict_steps больше 1 имеет смысл только при ordered")
	case k.ConflictSteps > 1 && k.ConflictSteps > len(k.Values)-1:
		add("conflict_steps %d больше числа ступеней %d", k.ConflictSteps, len(k.Values)-1)
	}
	if k.ConflictThetaHotelier < 0 {
		add("conflict_theta_hotelier не может быть отрицательным")
	}
	if k.ConflictTheta < 0 {
		add("conflict_theta не может быть отрицательным")
	}
	return out
}

func validateStrategies(c domain.Characteristic, tools []compute.Spec) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	if len(c.Strategies) == 0 && strings.TrimSpace(c.AskHotelier) == "" {
		add("нет ни стратегий, ни ask_hotelier: значение неоткуда взять")
	}
	type use struct{ tool, variant string }
	used := map[use]bool{}
	for i, s := range c.Strategies {
		where := fmt.Sprintf("стратегия %d (%s)", i+1, s.Tool)
		spec := findTool(tools, s.Tool)
		if spec == nil {
			add("%s: инструмента нет (есть: %s)", where, toolNames(tools))
			continue
		}
		if used[use{s.Tool, s.Variant}] {
			add("%s: инструмент и вариант уже использованы выше", where)
		}
		used[use{s.Tool, s.Variant}] = true
		if !spec.AnyKey && !slices.Contains(spec.Keys, c.Key) {
			add("%s: инструмент считает только %s", where, strings.Join(spec.Keys, ", "))
		}
		if spec.Scope == compute.ScopeDistrict && c.Level != domain.LevelDistrict {
			add("%s: инструмент считает по районам, а level %q", where, c.Level)
		}
		if spec.Scope == compute.ScopeHotel && c.Level == domain.LevelDistrict {
			add("%s: инструмент считает по отелям, а level district", where)
		}
		out = append(out, validateVariant(where, spec, s)...)
		if !(s.BaseReliability > 0 && s.BaseReliability < 1) {
			add("%s: base_reliability должна быть в (0; 1), получено %g", where, s.BaseReliability)
		}
		if strings.TrimSpace(s.IndependenceGroup) == "" {
			add("%s: нужен independence_group (источники одной группы считаются один раз)", where)
		}
		out = append(out, validateParams(where, spec, s)...)
	}
	return out
}

func validateVariant(where string, spec *compute.Spec, s domain.Strategy) []string {
	if len(spec.Variants) == 0 {
		if s.Variant != "" {
			return []string{fmt.Sprintf("%s: у инструмента нет вариантов, а strategy %q", where, s.Variant)}
		}
		return nil
	}
	ids := make([]string, len(spec.Variants))
	for i, v := range spec.Variants {
		ids[i] = v.ID
	}
	if !slices.Contains(ids, s.Variant) {
		return []string{fmt.Sprintf("%s: strategy %q, допустимо %s", where, s.Variant, strings.Join(ids, ", "))}
	}
	return nil
}

func validateParams(where string, spec *compute.Spec, s domain.Strategy) []string {
	var out []string
	known := map[string]bool{}
	for _, p := range spec.Params {
		known[p.Name] = true
	}
	for _, name := range sortedKeys(s.Params) {
		if !known[name] {
			out = append(out, fmt.Sprintf("%s: неизвестный параметр %s (есть: %s)", where, name, paramNames(spec)))
		}
	}
	sets := map[string]compute.SetParam{}
	for _, p := range spec.SetParams {
		sets[p.Name] = p
	}
	for _, name := range sortedKeys(s.Lists) {
		sp, ok := sets[name]
		if !ok {
			out = append(out, fmt.Sprintf("%s: неизвестный параметр-набор %s (есть: %s)", where, name, paramNames(spec)))
			continue
		}
		if len(s.Lists[name]) == 0 {
			out = append(out, fmt.Sprintf("%s: набор %s пуст", where, name))
		}
		for _, v := range s.Lists[name] {
			if !slices.Contains(sp.Choices, v) {
				out = append(out, fmt.Sprintf("%s: %s содержит %q, допустимо %s", where, name, v, strings.Join(sp.Choices, ", ")))
			}
		}
	}
	return out
}

// checkSharedParams: числовые параметры инструмента задаются реестром на весь инструмент (walk считает
// пляж и метро за один проход), поэтому один параметр у разных характеристик не может иметь разные значения.
// У универсальных инструментов (AnyKey) параметры у каждой характеристики свои.
func checkSharedParams(reg domain.Registry, tools []compute.Spec) []error {
	var errs []error
	for i := range tools {
		if tools[i].AnyKey {
			continue
		}
		seen := map[string]float64{}
		from := map[string]string{}
		for _, u := range reg.ToolUses(tools[i].Name) {
			for _, s := range u.Strategies {
				for k, v := range s.Params {
					if prev, ok := seen[k]; ok && prev != v {
						errs = append(errs, fmt.Errorf("инструмент %s: параметр %s=%g у %s и %g у %s",
							tools[i].Name, k, prev, from[k], v, u.Characteristic.Key))
					}
					seen[k], from[k] = v, u.Characteristic.Key
				}
			}
		}
	}
	return errs
}

func findTool(tools []compute.Spec, name string) *compute.Spec {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func toolNames(tools []compute.Spec) string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

func paramNames(spec *compute.Spec) string {
	var names []string
	for _, p := range spec.Params {
		names = append(names, p.Name)
	}
	for _, p := range spec.SetParams {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

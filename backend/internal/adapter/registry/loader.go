// Package registry загружает описания характеристик из YAML (реестр, раздел 7 PROJECT_CONTEXT.md).
package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// file — вид YAML-файла характеристики. Неизвестные поля — ошибка: опечатка не должна молча терять правило.
type file struct {
	Key              string         `yaml:"key"`
	Title            string         `yaml:"title"`
	Description      string         `yaml:"description"`
	Level            string         `yaml:"level"`
	ValueType        string         `yaml:"value_type"`
	Values           []string       `yaml:"values,omitempty"`
	Ordered          bool           `yaml:"ordered,omitempty"`
	ValueField       string         `yaml:"value_field,omitempty"`
	TTLDays          int            `yaml:"ttl_days"`
	FavorableValues  []string       `yaml:"favorable_values,omitempty"`
	SignificantDiff  float64        `yaml:"significant_diff,omitempty"`
	ConflictSteps    int            `yaml:"conflict_steps,omitempty"`
	ConflictTheta    float64        `yaml:"conflict_theta,omitempty"`
	ConflictEntities []string       `yaml:"conflict_entities,omitempty"`
	Strategies       []fileStrategy `yaml:"strategies,omitempty"`
	AskHotelier      string         `yaml:"ask_hotelier,omitempty"`
	AskGuest         string         `yaml:"ask_guest,omitempty"`
}

type fileStrategy struct {
	Tool              string         `yaml:"tool"`
	Strategy          string         `yaml:"strategy,omitempty"`
	Params            map[string]any `yaml:"params,omitempty"`
	BaseReliability   float64        `yaml:"base_reliability"`
	IndependenceGroup string         `yaml:"independence_group"`
}

// Parse разбирает один YAML-документ характеристики. Содержательную проверку делает usecase/registry.
func Parse(data []byte) (domain.Characteristic, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Characteristic{}, errors.New("пустой документ")
		}
		return domain.Characteristic{}, fmt.Errorf("YAML: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.Characteristic{}, errors.New("в файле должен быть один документ")
	}
	c := domain.Characteristic{
		Key: f.Key, Title: f.Title, Description: f.Description, Level: f.Level, ValueType: f.ValueType,
		Values: f.Values, Ordered: f.Ordered, ValueField: f.ValueField, TTLDays: f.TTLDays,
		FavorableValues: f.FavorableValues, SignificantDiff: f.SignificantDiff, ConflictSteps: f.ConflictSteps,
		ConflictTheta: f.ConflictTheta, ConflictEntities: f.ConflictEntities,
		AskHotelier: f.AskHotelier, AskGuest: f.AskGuest,
	}
	for i, s := range f.Strategies {
		st := domain.Strategy{
			Tool: s.Tool, Variant: s.Strategy, BaseReliability: s.BaseReliability, IndependenceGroup: s.IndependenceGroup,
		}
		var err error
		if st.Params, st.Lists, err = splitParams(s.Params); err != nil {
			return domain.Characteristic{}, fmt.Errorf("стратегия %d (%s): %w", i+1, s.Tool, err)
		}
		c.Strategies = append(c.Strategies, st)
	}
	return c, nil
}

// splitParams делит params на числа и наборы строк (kinds: [pharmacy]).
func splitParams(in map[string]any) (map[string]float64, map[string][]string, error) {
	var nums map[string]float64
	var lists map[string][]string
	names := make([]string, 0, len(in))
	for k := range in {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		switch v := in[k].(type) {
		case int:
			nums = put(nums, k, float64(v))
		case float64:
			nums = put(nums, k, v)
		case []any:
			var ss []string
			for _, x := range v {
				s, ok := x.(string)
				if !ok {
					return nil, nil, fmt.Errorf("параметр %s: в наборе допустимы только строки, получено %v", k, x)
				}
				ss = append(ss, s)
			}
			if lists == nil {
				lists = map[string][]string{}
			}
			lists[k] = ss
		default:
			return nil, nil, fmt.Errorf("параметр %s: ожидается число или список строк, получено %v", k, v)
		}
	}
	return nums, lists, nil
}

func put(m map[string]float64, k string, v float64) map[string]float64 {
	if m == nil {
		m = map[string]float64{}
	}
	m[k] = v
	return m
}

// Load читает все *.yaml каталога fsys и возвращает реестр; порядок — по имени файла.
// Имя файла должно совпадать с ключом характеристики: так файл находится без разбора.
func Load(fsys fs.FS) (domain.Registry, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return domain.Registry{}, err
	}
	sort.Strings(names)
	var cs []domain.Characteristic
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return domain.Registry{}, err
		}
		c, err := Parse(data)
		if err != nil {
			return domain.Registry{}, fmt.Errorf("%s: %w", name, err)
		}
		if want := strings.TrimSuffix(path.Base(name), ".yaml"); c.Key != want {
			return domain.Registry{}, fmt.Errorf("%s: key %q не совпадает с именем файла", name, c.Key)
		}
		cs = append(cs, c)
	}
	return domain.NewRegistry(cs)
}

// Marshal записывает характеристику в YAML, который читает Parse.
func Marshal(c domain.Characteristic) ([]byte, error) {
	f := file{
		Key: c.Key, Title: c.Title, Description: c.Description, Level: c.Level, ValueType: c.ValueType,
		Values: c.Values, Ordered: c.Ordered, ValueField: c.ValueField, TTLDays: c.TTLDays,
		FavorableValues: c.FavorableValues, SignificantDiff: c.SignificantDiff, ConflictSteps: c.ConflictSteps,
		ConflictTheta: c.ConflictTheta, ConflictEntities: c.ConflictEntities,
		AskHotelier: c.AskHotelier, AskGuest: c.AskGuest,
	}
	for _, s := range c.Strategies {
		fs := fileStrategy{Tool: s.Tool, Strategy: s.Variant, BaseReliability: s.BaseReliability, IndependenceGroup: s.IndependenceGroup}
		if len(s.Params)+len(s.Lists) > 0 {
			fs.Params = make(map[string]any, len(s.Params)+len(s.Lists))
			for k, v := range s.Params {
				fs.Params[k] = v
			}
			for k, v := range s.Lists {
				fs.Params[k] = v
			}
		}
		f.Strategies = append(f.Strategies, fs)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

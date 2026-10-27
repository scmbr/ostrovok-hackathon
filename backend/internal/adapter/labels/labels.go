// Пакет labels читает файлы разметки отелей (data/labels/*.yaml) для метрик качества.
package labels

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/metrics"
)

type file struct {
	LabeledAt string      `yaml:"labeled_at"`
	Labeler   string      `yaml:"labeler"`
	Hotels    []fileHotel `yaml:"hotels"`
}

type fileHotel struct {
	ID    int64               `yaml:"id"`
	Name  string              `yaml:"name"`
	Group string              `yaml:"group"`
	Marks map[string]fileMark `yaml:"marks"`
}

type fileMark struct {
	Value      *bool  `yaml:"value"`
	Confidence string `yaml:"confidence"`
	Source     string `yaml:"source"`
	Note       string `yaml:"note"`
}

// LoadDir читает все *.yaml из каталога и проверяет метки; ошибки собираются построчно по отелю и цели.
func LoadDir(dir string) ([]metrics.HotelLabel, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("в %s нет файлов разметки *.yaml", dir)
	}
	slices.Sort(paths)
	var out []metrics.HotelLabel
	seen := map[int64]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var f file
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&f); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		ls, err := convert(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, l := range ls {
			if prev, dup := seen[l.HotelID]; dup {
				return nil, fmt.Errorf("%s: отель %d уже размечен в %s", p, l.HotelID, prev)
			}
			seen[l.HotelID] = p
		}
		out = append(out, ls...)
	}
	return out, nil
}

func convert(f file) ([]metrics.HotelLabel, error) {
	if strings.TrimSpace(f.LabeledAt) == "" {
		return nil, fmt.Errorf("не задан labeled_at")
	}
	var out []metrics.HotelLabel
	var errs []string
	for _, h := range f.Hotels {
		l := metrics.HotelLabel{HotelID: h.ID, Name: h.Name, Group: h.Group, Marks: map[string]metrics.Mark{}}
		if h.ID <= 0 {
			errs = append(errs, "отель без id")
			continue
		}
		if h.Group != "coastal" && h.Group != "urban" {
			errs = append(errs, fmt.Sprintf("отель %d: group %q, допустимо coastal или urban", h.ID, h.Group))
		}
		for _, t := range metrics.Targets {
			m, ok := h.Marks[t]
			if !ok {
				errs = append(errs, fmt.Sprintf("отель %d: нет метки %s", h.ID, t))
				continue
			}
			switch m.Confidence {
			case metrics.Sure, metrics.Likely:
				if m.Value == nil {
					errs = append(errs, fmt.Sprintf("отель %d, %s: при уверенности %s нужно value", h.ID, t, m.Confidence))
				}
			case metrics.Unknown:
				if m.Value != nil {
					errs = append(errs, fmt.Sprintf("отель %d, %s: при unknown value не задаётся", h.ID, t))
				}
			default:
				errs = append(errs, fmt.Sprintf("отель %d, %s: confidence %q, допустимо sure, likely, unknown", h.ID, t, m.Confidence))
			}
			if strings.TrimSpace(m.Source) == "" {
				errs = append(errs, fmt.Sprintf("отель %d, %s: нет ссылки на источник", h.ID, t))
			}
			l.Marks[t] = metrics.Mark{Value: m.Value, Confidence: m.Confidence, Source: m.Source, Note: m.Note}
		}
		for t := range h.Marks {
			if !slices.Contains(metrics.Targets, t) {
				errs = append(errs, fmt.Sprintf("отель %d: неизвестная цель %q", h.ID, t))
			}
		}
		out = append(out, l)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

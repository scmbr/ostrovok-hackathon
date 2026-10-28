package registry

import (
	"bytes"
	"fmt"
	"io/fs"

	"gopkg.in/yaml.v3"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// PoiFile — имя файла словаря типов заведений; Load его пропускает: это не характеристика реестра.
const PoiFile = "poi_kinds.yaml"

type poiFile struct {
	Fillers []string      `yaml:"fillers"`
	Kinds   []poiKindFile `yaml:"kinds"`
}

type poiKindFile struct {
	ID         string            `yaml:"id"`
	Kinds      []string          `yaml:"kinds"`
	Label      string            `yaml:"label"`
	Labels     map[string]string `yaml:"labels,omitempty"`
	Title      string            `yaml:"title"`
	TitleMet   string            `yaml:"title_met"`
	TitleUnmet string            `yaml:"title_unmet"`
	Icon       string            `yaml:"icon"`
	Color      string            `yaml:"color"`
	Synonyms   []string          `yaml:"synonyms,omitempty"`
}

// LoadPoi читает словарь типов заведений из fsys. Содержательную проверку делает usecase/registry.ValidatePoi.
func LoadPoi(fsys fs.FS) (domain.PoiDict, error) {
	data, err := fs.ReadFile(fsys, PoiFile)
	if err != nil {
		return domain.PoiDict{}, err
	}
	var f poiFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return domain.PoiDict{}, fmt.Errorf("%s: YAML: %w", PoiFile, err)
	}
	kinds := make([]domain.PoiKind, len(f.Kinds))
	for i, k := range f.Kinds {
		kinds[i] = domain.PoiKind{
			ID: k.ID, Kinds: k.Kinds, Label: k.Label, Labels: k.Labels, Title: k.Title, TitleMet: k.TitleMet, TitleUnmet: k.TitleUnmet,
			Icon: k.Icon, Color: k.Color, Synonyms: k.Synonyms,
		}
	}
	d, err := domain.NewPoiDict(kinds, f.Fillers)
	if err != nil {
		return domain.PoiDict{}, fmt.Errorf("%s: %w", PoiFile, err)
	}
	return d, nil
}

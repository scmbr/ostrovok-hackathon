package registry

import (
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func deriveReg(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{{Key: "pets", Synonyms: []string{"собака", "питомец"}}})
	if err != nil {
		t.Fatal(err)
	}
	poi, err := domain.NewPoiDict([]domain.PoiKind{{ID: "cafe", Kinds: []string{"cafe"}, Synonyms: []string{"кафе"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg.WithPoi(poi)
}

// Фразы, пересекающиеся со словарём заведений, при выводе отбрасываются: «кафе» принадлежит near_poi.cafe.
func TestDeriveSkipsPoiOverlap(t *testing.T) {
	a := &Authoring{}
	syn, skipped := a.derive(deriveReg(t), "night_cafe", "Ночное кафе", "есть ли круглосуточное кафе")
	if len(syn) != 2 || syn[0] != "ночное" || syn[1] != "круглосуточное" {
		t.Errorf("синонимы: %q", syn)
	}
	if len(skipped) == 0 || !strings.Contains(strings.Join(skipped, "\n"), "near_poi.cafe") {
		t.Errorf("пояснения: %q", skipped)
	}
}

// кафе без других слов: синонимов не остаётся, это понятная ошибка с подсказкой, а не падение сохранения.
func TestDraftErrorWhenNoSynonymsLeft(t *testing.T) {
	a := &Authoring{}
	reg := deriveReg(t)
	c := domain.Characteristic{Key: "night_cafe", Title: "Кафе"}
	c.Synonyms, _ = a.derive(reg, c.Key, c.Title, "кафе")
	errs := a.check(reg, c)
	if !strings.Contains(strings.Join(errs, "\n"), "synonyms") {
		t.Fatalf("ожидалась ошибка про synonyms: %q", errs)
	}
	if !strings.Contains(strings.Join(errs, "\n"), "добавьте") {
		t.Errorf("нет подсказки: %q", errs)
	}
}

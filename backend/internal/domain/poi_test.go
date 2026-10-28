package domain_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	embedded "github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

func poiLexicon(t *testing.T) (domain.Registry, domain.Lexicon) {
	t.Helper()
	reg, err := adapterregistry.Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	poi, err := adapterregistry.LoadPoi(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	reg = reg.WithPoi(poi)
	return reg, domain.NewLexicon(reg)
}

func TestPoiPhrases(t *testing.T) {
	_, lex := poiLexicon(t)
	cases := []struct{ text, want string }{
		// Главная фраза: «ночными» не уходит в реестр и входит во фразу типа.
		{"рядом с ночными клубами", "near_poi.nightclub=300"},
		{"хочу ночной клуб поблизости", "near_poi.nightclub=300"},
		{"клуб в 150 метров", "near_poi.nightclub=150"},
		{"рядом с баром", "near_poi.bar=300"},
		{"отель недалеко от ресторана и кафе", "near_poi.restaurant=300 near_poi.cafe=300"},
		{"хочу тусить", "near_poi.nightlife=300"},
		{"бары и клубы рядом", "near_poi.nightlife=300"},
		{"без клубов", "-near_poi.nightclub"},
		{"без ночных клубов", "-near_poi.nightclub"},
		// Реестр сильнее словаря: эти фразы идут прежним путём.
		{"ночная аптека рядом", "night_pharmacy"},
		{"нужна аптека", "night_pharmacy"},
		{"тусовки", "night_services=rich"},
		{"ночная жизнь", "night_services=rich"},
		// Часы работы — не тип заведения.
		{"нужен круглосуточный магазин рядом", "| непонято: круглосуточный магазин"},
		{"круглосуточное кафе", "| непонято: круглосуточное кафе"},
	}
	for _, c := range cases {
		got := briefPoi(lex.Parse(c.text))
		if got != c.want {
			t.Errorf("%q: получено %q, ожидалось %q", c.text, got, c.want)
		}
	}
}

// briefPoi — желания near_poi и прочие одной строкой; «| непонято: …» — остаток.
func briefPoi(r domain.ParseResult) string {
	var parts []string
	for _, w := range r.Wishes {
		s := w.Key
		if w.Value != nil {
			s += "=" + formatFloat(w.Value)
		}
		parts = append(parts, s)
	}
	for _, k := range r.Remove {
		parts = append(parts, "-"+k)
	}
	out := strings.Join(parts, " ")
	if len(r.Unmet) > 0 {
		out += "| непонято: " + strings.Join(r.Unmet, "; ")
	}
	return out
}

func formatFloat(v any) string {
	switch x := v.(type) {
	case float64:
		return strconvFloat(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return strings.TrimSpace(strings.Trim(strings.ToLower(strings.TrimSpace(sprint(v))), "\""))
}

func TestEvalNearPoi(t *testing.T) {
	reg, _ := poiLexicon(t)
	c, ok := reg.Get("near_poi.nightclub")
	if !ok {
		t.Fatal("виртуальная характеристика near_poi.nightclub не найдена")
	}
	w := domain.Desire{Key: c.Key, Value: 300.0, Op: domain.OpLe, Importance: domain.Prefer}
	at := func(d float64) *domain.NearbyPoi {
		return &domain.NearbyPoi{Kind: "nightclub", Label: "клуб", DistM: d}
	}

	cases := []struct {
		name   string
		near   *domain.NearbyPoi
		status domain.Status
	}{
		{"180 м — совпало", at(180), domain.StatusMatch},
		{"250 м — ещё совпало", at(250), domain.StatusMatch},
		{"275 м — плавная полоса", at(275), domain.StatusNear},
		{"300 м — на границе", at(300), domain.StatusNear},
		{"не нашли", nil, domain.StatusMismatch},
	}
	for _, tc := range cases {
		e := domain.EvalNearPoi(c, w, tc.near)
		if e.Status != tc.status {
			t.Errorf("%s: статус %s, ожидался %s", tc.name, e.Status, tc.status)
		}
		if e.Confidence != domain.PoiConfidence {
			t.Errorf("%s: уверенность %v, ожидалась база OSM %v", tc.name, e.Confidence, domain.PoiConfidence)
		}
	}

	// Внутри полосы вероятность падает плавно, на границе радиуса 0,5.
	p250 := domain.EvalNearPoi(c, w, at(250)).P
	p275 := domain.EvalNearPoi(c, w, at(275)).P
	p300 := domain.EvalNearPoi(c, w, at(300)).P
	if !(p250 == 1 && p275 > p300 && p275 < p250 && p300 == 0.5) {
		t.Errorf("полоса: p(250)=%v p(275)=%v p(300)=%v", p250, p275, p300)
	}

	// Объяснение: расстояние по прямой, источник OSM; «не нашли» — не «далеко».
	if got := domain.ExplainWish(c, domain.EvalNearPoi(c, w, at(180))); !strings.Contains(got, "клуб в 180 м по прямой") || !strings.Contains(got, "OSM") {
		t.Errorf("объяснение: %q", got)
	}
	got := domain.ExplainWish(c, domain.EvalNearPoi(c, w, nil))
	if !strings.Contains(got, "в OSM не нашли в 300 м") || strings.Contains(got, "далеко") {
		t.Errorf("объяснение «не нашли»: %q", got)
	}
	// Обязательное желание без заведения нарушено.
	must := w
	must.Importance = domain.Must
	if !domain.EvalNearPoi(c, must, nil).Violated() {
		t.Error("must без заведения в радиусе должен быть нарушен")
	}
}

func TestNearPoiNormalizeClampsRadius(t *testing.T) {
	reg, _ := poiLexicon(t)
	d, _, err := domain.NormalizeWish(reg, domain.Desire{Key: "near_poi.bar", Value: 900.0})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Value.(float64); v != domain.PoiRadiusM {
		t.Errorf("радиус %v, ожидалось не больше %v (данных дальше нет)", d.Value, domain.PoiRadiusM)
	}
	if _, _, err := domain.NormalizeWish(reg, domain.Desire{Key: "near_poi.bar", Value: 100.0, Op: domain.OpGe}); err == nil {
		t.Error("ge для near_poi должно отклоняться")
	}
}

// Виртуальные желания не попадают в перечень реестра, который видят агенты и API.
func TestPoiNotInRegistryAll(t *testing.T) {
	reg, _ := poiLexicon(t)
	for _, c := range reg.All() {
		if domain.IsNearPoiKey(c.Key) {
			t.Errorf("%s попала в Registry.All()", c.Key)
		}
	}
}

func strconvFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func sprint(v any) string { return fmt.Sprint(v) }

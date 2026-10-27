package domain_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
	embedded "github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

func testLexicon(t *testing.T) (domain.Registry, domain.Lexicon) {
	t.Helper()
	reg, err := adapterregistry.Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	return reg, domain.NewLexicon(reg)
}

// brief — результат разбора одной строкой: желания (ключ=значение, ! — must, @этаж), фильтры, исключённое,
// неоднозначность, «похожий», непонятое.
func brief(r domain.ParseResult) string {
	var parts []string
	for _, w := range r.Wishes {
		s := w.Key
		if w.Value != nil {
			s += fmt.Sprintf("=%v", w.Value)
		}
		if w.Importance == domain.Must {
			s += "!"
		}
		if w.MinFloor > 0 {
			s += fmt.Sprintf("@%d", w.MinFloor)
		}
		parts = append(parts, s)
	}
	if r.Filters.PriceMax > 0 {
		parts = append(parts, fmt.Sprintf("price<=%d", r.Filters.PriceMax))
	}
	if r.Filters.StarsMin > 0 {
		parts = append(parts, fmt.Sprintf("stars>=%d", r.Filters.StarsMin))
	}
	for _, k := range r.Remove {
		parts = append(parts, "-"+k)
	}
	for _, a := range r.Ambiguities {
		var ks []string
		for _, o := range a.Options {
			ks = append(ks, o.Key)
		}
		parts = append(parts, "?("+strings.Join(ks, "|")+")")
	}
	if r.Similar != nil {
		parts = append(parts, fmt.Sprintf("similar(%d,%v,%q)", r.Similar.Ordinal, r.Similar.Current, r.Similar.Name))
	}
	if len(r.Unmet) > 0 {
		parts = append(parts, fmt.Sprintf("unmet%q", r.Unmet))
	}
	return strings.Join(parts, " ")
}

func TestParsePhrases(t *testing.T) {
	_, lex := testLexicon(t)
	for _, tt := range []struct{ text, want string }{
		// полностью разобрано
		{"тихий номер с видом на море", "noise_night=quiet sea_view"},
		{"до пляжа не больше 10 минут и с собакой", "walk_to_beach=10 pets_allowed"},
		{"тихо ночью, затемняющие шторы", "noise_night=quiet blackout_curtains"},
		{"шторы затемняющие и тихо", "blackout_curtains noise_night=quiet"},
		{"вид на море, бюджет до 150 €", "sea_view price<=150"},
		{"рядом ночная аптека, до метро не больше 5 минут", "night_pharmacy walk_to_metro=5"},
		{"обязательно тихо и с питомцем", "noise_night=quiet! pets_allowed!"}, // маркер действует на всю часть до запятой
		{"обязательно тихо, и с питомцем", "noise_night=quiet! pets_allowed"},
		{"главное — вид на море, а ещё метро в 7 минутах", "sea_view! walk_to_metro=7"},
		{"хочу выспаться, бюджет 120 евро", "noise_night=quiet price<=120"},
		{"с 4-го этажа вид на море", "sea_view@4"},
		{"отель 4 звезды, пляж рядом", "walk_to_beach=10 stars>=4"},
		{"не шумно и метро близко", "noise_night=quiet walk_to_metro=10"},
		{"в гуще событий, ночная жизнь", "night_services=rich"},
		{"500 метров до пляжа", "walk_to_beach=7"},
		{"желательно пляж не дальше 15 мин", "walk_to_beach=15"},
		// отрицание и исключение
		{"без пляжа", "-walk_to_beach"},
		{"не нужен пляж, но тихо", "noise_night=quiet -walk_to_beach"},
		{"без шума и без собаки", "noise_night=quiet -pets_allowed"},
		// неоднозначность
		{"хочу у моря, обязательно тихо", "noise_night=quiet! ?(sea_view|walk_to_beach)"},
		// частично разобрано
		{"тихий номер с видом на закат недалеко от моря", `noise_night=quiet ?(sea_view|walk_to_beach) unmet["видом на закат"]`},
		// не разобрано
		{"морской воздух", `unmet["морской воздух"]`},
		{"в метре от моря", `unmet["метре от моря"]`},
		{"привет, подбери отель в Барселоне", ""},
		{"нужен балкон с джакузи", `unmet["балкон с джакузи"]`},
	} {
		t.Run(tt.text, func(t *testing.T) {
			if got := brief(lex.Parse(tt.text)); got != tt.want {
				t.Errorf("%q\n получили %q\n ждали    %q", tt.text, got, tt.want)
			}
		})
	}
}

// Слова до пяти букв сопоставляются целиком: «метров» и «метре» — не «метро», «морской» — не «море».
func TestParseShortSynonymsMatchWholeWords(t *testing.T) {
	_, lex := testLexicon(t)
	r := lex.Parse("500 метров до пляжа")
	if len(r.Wishes) != 1 || r.Wishes[0].Key != "walk_to_beach" {
		t.Fatalf("500 метров до пляжа: %+v", r.Wishes)
	}
	if got := r.Wishes[0].Value; got != 7.0 {
		t.Errorf("500 м при 80 м/мин — 7 минут, получили %v", got)
	}
	if len(r.Notes) == 0 || !strings.Contains(r.Notes[0], "500 м") {
		t.Errorf("перевод метров в минуты должен быть назван: %v", r.Notes)
	}
	for _, text := range []string{"в метре от моря", "морской воздух", "метров десять до отеля"} {
		r := lex.Parse(text)
		for _, w := range r.Wishes {
			t.Errorf("%q: желание %q не должно появиться", text, w.Key)
		}
		if len(r.Ambiguities) > 0 {
			t.Errorf("%q: неоднозначности быть не должно", text)
		}
	}
	if r := lex.Parse("морской воздух"); len(r.Unmet) != 1 || r.Unmet[0] != "морской воздух" {
		t.Errorf("«морской воздух» должен уйти в unmet_wishes: %v", r.Unmet)
	}
	// формы длинных синонимов — по первым пяти буквам
	for _, text := range []string{"с собакой", "с собаками", "с питомцем", "возьмём питомца"} {
		if r := lex.Parse(text); len(r.Wishes) != 1 || r.Wishes[0].Key != "pets_allowed" {
			t.Errorf("%q: %+v", text, r.Wishes)
		}
	}
}

func TestParseAmbiguityNamesOptionsAndDefault(t *testing.T) {
	_, lex := testLexicon(t)
	r := lex.Parse("номер у моря")
	if len(r.Ambiguities) != 1 || len(r.Wishes) != 0 {
		t.Fatalf("ждали одну неоднозначность: %+v", r)
	}
	a := r.Ambiguities[0]
	if a.Phrase != "у моря" || len(a.Options) != 2 {
		t.Fatalf("%+v", a)
	}
	def := ""
	for _, o := range a.Options {
		if o.Default {
			def = o.Key
		}
	}
	if def != "walk_to_beach" {
		t.Errorf("«у моря» по умолчанию — пляж, а не вид: %q", def)
	}
	labels := map[string]string{}
	for _, o := range a.Options {
		labels[o.Key] = o.Label
		if o.Key == "walk_to_beach" && o.Desire.Value != 10.0 {
			t.Errorf("пляж рядом — до 10 минут: %+v", o.Desire)
		}
	}
	if labels["sea_view"] != "вид из окна" || labels["walk_to_beach"] != "пляж рядом (до 10 минут)" {
		t.Errorf("подписи трактовок: %v", labels)
	}
	// важность, названная гостем, переходит в трактовки
	if got := lex.Parse("обязательно у моря").Ambiguities[0].Options[0].Desire.Importance; got != domain.Must {
		t.Errorf("важность трактовки: %v", got)
	}
	// явное название снимает неоднозначность
	if r := lex.Parse("у моря, с видом на море"); len(r.Ambiguities) != 0 || len(r.Wishes) != 1 {
		t.Errorf("явное «вид на море» снимает вопрос: %+v", r)
	}
}

func TestParseImportanceMarkers(t *testing.T) {
	_, lex := testLexicon(t)
	if r := lex.Parse("обязательно тихо"); r.Wishes[0].Importance != domain.Must {
		t.Error("«обязательно» — must")
	}
	if r := lex.Parse("тихо, главное вид на море"); r.Wishes[0].Importance != domain.Prefer || r.Wishes[1].Importance != domain.Must {
		t.Errorf("маркер действует в своей части: %+v", r.Wishes)
	}
	if r := lex.Parse("не обязательно, но тихо"); r.Wishes[0].Importance != domain.Prefer {
		t.Errorf("«не обязательно»: %+v", r.Wishes)
	}
	r := lex.Parse("мне нужен тихий номер")
	if r.Wishes[0].Importance != domain.Prefer || !slices.Equal(r.WeakKeys, []string{"noise_night"}) {
		t.Errorf("«нужен» — мягкий маркер, важность неоднозначна: %+v %v", r.Wishes, r.WeakKeys)
	}
	if r := lex.Parse("обязательно нужен тихий номер"); len(r.WeakKeys) != 0 || r.Wishes[0].Importance != domain.Must {
		t.Errorf("явное «обязательно» снимает неоднозначность: %+v %v", r.Wishes, r.WeakKeys)
	}
}

func TestParseSimilarAndReset(t *testing.T) {
	_, lex := testLexicon(t)
	for text, want := range map[string]domain.SimilarRef{
		"похожий на этот":                {Current: true},
		"похожий на второй":              {Ordinal: 2},
		"покажи похожий на 3-й":          {Ordinal: 3},
		"похожий на Hotel Arts, но тише": {Name: "hotel arts"},
	} {
		r := lex.Parse(text)
		if r.Similar == nil || *r.Similar != want {
			t.Errorf("%q: %+v, ждали %+v", text, r.Similar, want)
		}
	}
	if r := lex.Parse("давай заново: тихо"); !r.Reset || len(r.Wishes) != 1 {
		t.Errorf("сброс: %+v", r)
	}
	if got := brief(lex.Parse("похожий на этот, но тише")); got != `noise_night=quiet similar(0,true,"")` {
		t.Errorf("«но тише» после «похожий»: %q", got)
	}
}

// Пять запросов проверки 6.1 разбираются в те же желания, что записаны в search.DemoQueries
// (важность — по маркеру в тексте: без него prefer, с «обязательно» — must).
func TestParseDemoQueriesFromSearch6_1(t *testing.T) {
	reg, lex := testLexicon(t)
	norm := func(ds []domain.Desire) []domain.Desire {
		var out []domain.Desire
		for _, d := range ds {
			n, _, err := domain.NormalizeWish(reg, d)
			if err != nil {
				t.Fatal(err)
			}
			n.Importance = ""
			out = append(out, n)
		}
		return out
	}
	for _, dq := range search.DemoQueries {
		r := lex.Parse(dq.Text)
		if len(r.Unmet) != 0 || len(r.Ambiguities) != 0 {
			t.Errorf("%q: не всё разобрано: unmet %v, неоднозначность %v", dq.Text, r.Unmet, r.Ambiguities)
		}
		if got, want := norm(r.Wishes), norm(dq.Query.Wishes); !reflect.DeepEqual(got, want) {
			t.Errorf("%q:\n получили %+v\n ждали    %+v", dq.Text, got, want)
		}
		if r.Filters != dq.Query.Filters {
			t.Errorf("%q: фильтры %+v, ждали %+v", dq.Text, r.Filters, dq.Query.Filters)
		}
		// маркер действует на свою часть до запятой: ставим его перед каждой
		must := strings.ReplaceAll("обязательно "+dq.Text, ", ", ", обязательно ")
		for _, w := range lex.Parse(must).Wishes {
			if w.Importance != domain.Must {
				t.Errorf("%q: с «обязательно» желание %s должно быть must", must, w.Key)
			}
		}
	}
}

// Желания разбора проходят схему реестра: поиск их не отклонит.
func TestParsedWishesPassRegistrySchema(t *testing.T) {
	reg, lex := testLexicon(t)
	for _, text := range []string{"с 4-го этажа вид на море", "тихо, пляж рядом, метро, собака, ночная аптека, шторы, ночная жизнь", "тише"} {
		for _, w := range lex.Parse(text).Wishes {
			if _, _, err := domain.NormalizeWish(reg, w); err != nil {
				t.Errorf("%q: %v", text, err)
			}
		}
	}
}

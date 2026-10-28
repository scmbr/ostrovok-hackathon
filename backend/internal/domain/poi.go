package domain

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// «Рядом с …»: желание о типе заведения (клуб, бар, кафе) считается по запросу из таблицы pois, значение в facts не пишется.
// Словарь типов — registry/poi_kinds.yaml; ключ желания — NearPoiPrefix + id типа. Описание — docs/search.md.

const (
	// NearPoiPrefix — префикс ключа желания «рядом с заведением»: near_poi.nightclub.
	NearPoiPrefix = "near_poi."
	// PoiRadiusM — pois загружены только в этом радиусе от отеля, дальше данных нет; больше радиус желания быть не может.
	PoiRadiusM = 300.0
	// PoiBandM — плавная полоса внутри радиуса: до PoiRadiusM−PoiBandM «совпало», дальше вероятность падает до 0,5 на границе.
	PoiBandM = 50.0
	// PoiConfidence — уверенность расчёта по OSM (база источника OSM в реестре).
	PoiConfidence = 0.6
	// PoiFieldName — поле значения виртуальной характеристики: расстояние по прямой, метры.
	PoiFieldName = "distance_m"
)

// IsNearPoiKey сообщает, что ключ — желание «рядом с заведением».
func IsNearPoiKey(key string) bool { return strings.HasPrefix(key, NearPoiPrefix) }

// NearPoiKey — ключ желания для типа заведения.
func NearPoiKey(id string) string { return NearPoiPrefix + id }

// PoiKind — тип заведения словаря: одно значение pois.kind или группа («ночная жизнь» = клуб, бар, паб).
type PoiKind struct {
	ID string
	// Kinds — значения pois.kind, из которых берётся ближайшее заведение.
	Kinds []string
	// Label — как назвать заведение в тексте («клуб»); Labels уточняет по значению kind внутри группы.
	Label  string
	Labels map[string]string
	// Title — название желания («Клуб рядом»); TitleMet и TitleUnmet — формы для заголовков групп выдачи.
	Title, TitleMet, TitleUnmet string
	Icon, Color                 string
	// Synonyms — фразы гостя; пусто — по этому типу нельзя искать из текста (аптека: её фразы у night_pharmacy).
	Synonyms []string
}

// LabelOf — подпись заведения по значению pois.kind: «бар», «клуб»; запасной вариант — Label типа.
func (k PoiKind) LabelOf(kind string) string {
	if l, ok := k.Labels[kind]; ok {
		return l
	}
	if k.Label != "" {
		return k.Label
	}
	return kind
}

// Characteristic — виртуальная характеристика желания: число (метры по прямой), уровень отеля, в реестр не входит.
func (k PoiKind) Characteristic() Characteristic {
	return Characteristic{
		Key: NearPoiKey(k.ID), Title: k.Title, Description: "Расстояние по прямой до ближайшего заведения типа «" + k.Label + "» по OSM",
		Level: LevelHotel, ValueType: ValueNumber, ValueField: PoiFieldName, SignificantDiff: PoiBandM, DefaultNumber: PoiRadiusM,
		TitleMet: k.TitleMet, TitleUnmet: k.TitleUnmet, Synonyms: k.Synonyms, DayPart: "night",
	}
}

// PoiDict — словарь типов заведений.
type PoiDict struct {
	items []PoiKind
	index map[string]int
	// Fillers — прилагательные («ночной»), которые идут перед названием типа и входят во фразу: «ночных клубов».
	Fillers []string
}

// NewPoiDict собирает словарь; id должны быть уникальны.
func NewPoiDict(kinds []PoiKind, fillers []string) (PoiDict, error) {
	d := PoiDict{items: slices.Clone(kinds), index: make(map[string]int, len(kinds)), Fillers: slices.Clone(fillers)}
	for i, k := range d.items {
		if _, dup := d.index[k.ID]; dup {
			return PoiDict{}, fmt.Errorf("тип заведения %q описан дважды", k.ID)
		}
		d.index[k.ID] = i
	}
	return d, nil
}

// All — типы в порядке описания.
func (d PoiDict) All() []PoiKind { return slices.Clone(d.items) }

// Get — тип по id.
func (d PoiDict) Get(id string) (PoiKind, bool) {
	i, ok := d.index[id]
	if !ok {
		return PoiKind{}, false
	}
	return d.items[i], true
}

// ByKey — тип по ключу желания (near_poi.bar).
func (d PoiDict) ByKey(key string) (PoiKind, bool) {
	id, ok := strings.CutPrefix(key, NearPoiPrefix)
	if !ok {
		return PoiKind{}, false
	}
	return d.Get(id)
}

// WithPoi возвращает реестр со словарём заведений: Get видит желания near_poi.*, All их не перечисляет
// (агенты и API реестра работают как раньше).
func (r Registry) WithPoi(d PoiDict) Registry {
	r.poi = d
	return r
}

// Poi — словарь заведений реестра.
func (r Registry) Poi() PoiDict { return r.poi }

// NearbyPoi — ближайшее заведение типа в радиусе PoiRadiusM.
type NearbyPoi struct {
	OSMID int64
	// Kind — значение pois.kind (bar), Label — подпись («бар»), Name — название из OSM, может быть пустым.
	Kind, Label, Name string
	DistM             float64
	Lat, Lon          float64
}

// EvalNearPoi оценивает желание «рядом с <тип>» по ближайшему заведению. near == nil — в радиусе поиска ничего не найдено:
// это не «данных нет», а «в OSM не нашли» (оговорка про неполноту OSM — в уверенности 0,6). Радиус желания не больше PoiRadiusM;
// до радиуса минус полоса — «совпало», внутри полосы вероятность плавно падает до 0,5 на границе радиуса.
func EvalNearPoi(c Characteristic, w Desire, near *NearbyPoi) WishEval {
	radius, _ := toFloat(w.Value)
	if radius <= 0 || radius > PoiRadiusM {
		radius = PoiRadiusM
	}
	e := WishEval{Key: w.Key, Importance: w.Importance, Confidence: PoiConfidence}
	if near == nil {
		e.Status = StatusMismatch
		e.Caveat = fmt.Sprintf("в OSM не нашли в %s м", formatNum(radius))
		e.Score = WishScore(0, PoiConfidence)
		return e
	}
	d := math.Round(near.DistM)
	e.Value, e.Nearby = d, near
	p := NumberProbability(c, OpLe, d, radius)
	e.P, e.PBase = p, p
	switch {
	case p >= MatchProbability:
		e.Status = StatusMatch
	case p >= ViolationProbability:
		e.Status = StatusNear
		e.Caveat = fmt.Sprintf("%s в %s м по прямой, на границе", near.Label, formatNum(d))
	default:
		e.Status = StatusMismatch
	}
	e.Score = WishScore(p, PoiConfidence)
	return e
}

// explainNearPoi — строка объяснения желания «рядом с …»: расстояние по прямой и источник (OSM).
func explainNearPoi(c Characteristic, e WishEval) string {
	title := c.Title
	if title == "" {
		title = c.Key
	}
	var b strings.Builder
	b.WriteString(title)
	switch {
	case e.Nearby == nil:
		b.WriteString(": " + e.Caveat)
	case e.Status == StatusMatch:
		fmt.Fprintf(&b, ": совпало (%s в %s м по прямой)", e.Nearby.Label, formatNum(e.DistM()))
	case e.Status == StatusNear:
		fmt.Fprintf(&b, ": на границе — %s в %s м по прямой", e.Nearby.Label, formatNum(e.DistM()))
	default:
		fmt.Fprintf(&b, ": не совпало (%s в %s м по прямой)", e.Nearby.Label, formatNum(e.DistM()))
	}
	fmt.Fprintf(&b, "; уверенность %s, OSM", percent(e.Confidence))
	return b.String()
}

// DistM — расстояние до найденного заведения, м; 0, если заведение не найдено.
func (e WishEval) DistM() float64 {
	if e.Nearby == nil {
		return 0
	}
	return math.Round(e.Nearby.DistM)
}

// PoiOverlaps перечисляет фразы типов заведений, пересекающиеся (по правилу парсера) с фразами характеристик реестра
// или друг с другом. Реестр идёт первым в разборе, но пересечение всё равно ошибка: фраза не должна зависеть от порядка.
func PoiOverlaps(reg Registry) []SynonymOverlap {
	var out []SynonymOverlap
	regEntries := synonymEntries(reg)
	var own []phraseEntry
	for _, k := range reg.poi.items {
		for _, p := range k.Synonyms {
			if w := PhraseWords(p); len(w) > 0 {
				own = append(own, phraseEntry{key: NearPoiKey(k.ID), phrase: strings.Join(w, " "), words: w})
			}
		}
	}
	for i, a := range own {
		for _, b := range regEntries {
			if containsPhrase(a.words, b.words) || containsPhrase(b.words, a.words) {
				out = append(out, SynonymOverlap{a.key, a.phrase, b.key, b.phrase})
			}
		}
		for _, b := range own[i+1:] {
			// Внутри словаря длинная фраза (группа «бары и клубы») вправе содержать короткую («бары»): разбор идёт от длинных
			// к коротким. Спор возможен только у фраз одной длины.
			if a.key != b.key && len(a.words) == len(b.words) && (containsPhrase(a.words, b.words) || containsPhrase(b.words, a.words)) {
				out = append(out, SynonymOverlap{a.key, a.phrase, b.key, b.phrase})
			}
		}
	}
	return out
}

// WithoutOverlaps убирает фразы типов, пересекающиеся с фразами характеристик reg, и возвращает убранные. Реестр в разборе
// идёт первым и забрал бы такую фразу себе; у характеристик, добавленных текстом, это не повод останавливать запуск.
func (d PoiDict) WithoutOverlaps(reg Registry) (PoiDict, []SynonymOverlap) {
	regEntries := synonymEntries(reg)
	out := PoiDict{items: make([]PoiKind, len(d.items)), index: d.index, Fillers: d.Fillers}
	var dropped []SynonymOverlap
	for i, k := range d.items {
		kept := make([]string, 0, len(k.Synonyms))
		for _, p := range k.Synonyms {
			words := PhraseWords(p)
			clash := false
			for _, e := range regEntries {
				if containsPhrase(words, e.words) || containsPhrase(e.words, words) {
					dropped = append(dropped, SynonymOverlap{KeyA: NearPoiKey(k.ID), PhraseA: strings.Join(words, " "), KeyB: e.key, PhraseB: e.phrase})
					clash = true
					break
				}
			}
			if !clash {
				kept = append(kept, p)
			}
		}
		k.Synonyms = kept
		out.items[i] = k
	}
	return out, dropped
}

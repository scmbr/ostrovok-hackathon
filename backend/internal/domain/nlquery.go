package domain

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Детерминированный разбор фразы гостя в структурный запрос (docs/dialog.md). Работает без LLM: жюри вводит свои
// фразы и без ключа. Характеристики, синонимы и значения берутся из реестра; в коде только язык: маркеры важности,
// отрицания, единицы измерения, служебные слова.

// SimilarRef — «похожий на …»: порядковый номер в последней выдаче, текущий отель («на этот») или название.
type SimilarRef struct {
	Ordinal int // 1.. — номер в последней выдаче; 0 — не задан
	Current bool
	Name    string
}

// Match — какая фраза гостя каким желанием понята (для журнала и «под капотом»).
type Match struct {
	Phrase    string `json:"phrase"`
	Key       string `json:"key"`
	Negated   bool   `json:"negated,omitempty"`
	Ambiguous bool   `json:"ambiguous,omitempty"`
}

// Interpretation — одна трактовка неоднозначной фразы.
type Interpretation struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Default bool   `json:"default,omitempty"`
	Desire  Desire `json:"desire"`
}

// Ambiguity — фраза, которую можно понять по-разному («у моря» — вид или пляж).
type Ambiguity struct {
	Phrase  string           `json:"phrase"`
	Options []Interpretation `json:"options"`
}

// ParseResult — итог разбора одного сообщения.
type ParseResult struct {
	Wishes  []Desire
	Filters Filters
	// MinFloor — «с 4-го этажа»: уже подставлен в желания фасада из этого сообщения, а сессия применяет его и к прежним.
	MinFloor int
	// Remove — ключи, которые гость исключил («без пляжа»).
	Remove []string
	// Ambiguities — фразы с несколькими трактовками; в Wishes они не входят.
	Ambiguities []Ambiguity
	// WeakKeys — желания с мягким маркером важности («нужен», «важно»): гость мог иметь в виду и must, и prefer.
	WeakKeys []string
	// Unmet — куски, которым не нашлось характеристики реестра.
	Unmet   []string
	Matches []Match
	Similar *SimilarRef
	Reset   bool
	// Notes — допущения разбора, которые стоит назвать гостю («500 м ≈ 7 мин пешком»).
	Notes []string
}

// Empty сообщает, что в сообщении не понято ничего.
func (r ParseResult) Empty() bool {
	return len(r.Wishes) == 0 && len(r.Remove) == 0 && len(r.Ambiguities) == 0 && r.Similar == nil && !r.Reset &&
		r.Filters == (Filters{}) && r.MinFloor == 0
}

type lexEntry struct {
	words []string
	key   string
	// value — значение желания из value_synonyms; hasValue=false — значение по умолчанию.
	value    any
	hasValue bool
	amb      []Interpretation // не пусто — фраза неоднозначна
	phrase   string
	poi      bool // тип заведения из словаря registry/poi_kinds.yaml, а не характеристика реестра
}

// Lexicon — словарь фраз, построенный по реестру.
type Lexicon struct {
	reg     Registry
	entries []lexEntry
	// poiEntries — фразы типов заведений: разбираются после реестра, только по словам, которые реестр не взял.
	poiEntries []lexEntry
}

// NewLexicon строит словарь по synonyms, value_synonyms и ambiguous_synonyms реестра. Длинные фразы идут первыми.
func NewLexicon(reg Registry) Lexicon {
	l := Lexicon{reg: reg}
	amb := map[string]*lexEntry{}
	var ambOrder []string
	for _, c := range reg.All() {
		add := func(phrase string, value any, hasValue bool) {
			if w := PhraseWords(phrase); len(w) > 0 {
				l.entries = append(l.entries, lexEntry{words: w, key: c.Key, value: value, hasValue: hasValue, phrase: strings.Join(w, " ")})
			}
		}
		for _, p := range c.Synonyms {
			add(p, nil, false)
		}
		for _, v := range sortedStringKeys(c.ValueSynonyms) {
			for _, p := range c.ValueSynonyms[v] {
				add(p, synonymValue(c, v), true)
			}
		}
		for _, a := range c.AmbiguousSynonyms {
			w := PhraseWords(a.Phrase)
			if len(w) == 0 {
				continue
			}
			p := strings.Join(w, " ")
			e := amb[p]
			if e == nil {
				e = &lexEntry{words: w, phrase: p}
				amb[p] = e
				ambOrder = append(ambOrder, p)
			}
			d := Desire{Key: c.Key}
			if a.Value != nil {
				d.Value = a.Value
				if f, ok := toFloat(a.Value); ok && c.ValueType == ValueNumber {
					d.Value = f
				}
			} else {
				d = DefaultDesire(c)
			}
			e.amb = append(e.amb, Interpretation{Key: c.Key, Label: a.Label, Default: a.Default, Desire: d})
		}
	}
	for _, p := range ambOrder {
		l.entries = append(l.entries, *amb[p])
	}
	for _, k := range reg.poi.items {
		for _, p := range k.Synonyms {
			if w := PhraseWords(p); len(w) > 0 {
				l.poiEntries = append(l.poiEntries, lexEntry{words: w, key: NearPoiKey(k.ID), phrase: strings.Join(w, " "), poi: true})
			}
		}
	}
	byLength := func(es []lexEntry) {
		sort.SliceStable(es, func(i, j int) bool {
			a, b := es[i], es[j]
			if len(a.words) != len(b.words) {
				return len(a.words) > len(b.words)
			}
			return len(strings.Join(a.words, "")) > len(strings.Join(b.words, ""))
		})
	}
	byLength(l.entries)
	byLength(l.poiEntries)
	return l
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// synonymValue переводит ключ value_synonyms в значение желания: у bool — true / false.
func synonymValue(c Characteristic, v string) any {
	if c.ValueType == ValueBool {
		return v == "true"
	}
	return v
}

// DefaultDesire — желание по характеристике без уточнений гостя: значение берётся из реестра.
func DefaultDesire(c Characteristic) Desire {
	d := Desire{Key: c.Key}
	switch {
	case CategoryLevel(c):
	case c.ValueType == ValueEnum && len(c.FavorableValues) > 0:
		d.Value = c.FavorableValues[0]
	case c.ValueType == ValueNumber && c.DefaultNumber > 0:
		d.Value = c.DefaultNumber
	}
	return d
}

var (
	digitsWord  = regexp.MustCompile(`^\d+$`)
	clauseSep   = regexp.MustCompile(`[.,;:!?\n()]+`)
	digitLetter = regexp.MustCompile(`(\d)(\p{L})`)
	letterDigit = regexp.MustCompile(`(\p{L})(\d)`)
)

// Слова, которыми сообщение делится на части: маркеры важности и отрицание действуют внутри части.
var clauseBreakWords = map[string]bool{"чтобы": true, "плюс": true, "также": true, "еще": true, "а": true}

var (
	negators = map[string]bool{"без": true, "не": true, "нет": true, "убери": true, "убрать": true, "исключи": true, "исключить": true}
	// negVerbs после «не»: «не нужен пляж», «не хочу шум».
	negVerbs = map[string]bool{"нужен": true, "нужна": true, "нужно": true, "нужны": true, "надо": true, "важен": true, "важна": true, "важно": true, "хочу": true}
	// strongMarkers делают желание обязательным.
	strongMarkers = []string{"обязательно", "главное", "только", "строго", "непременно", "необходимо", "критично"}
	preferMarkers = []string{"желательно", "лучше", "можно", "возможно"}
	// weakMarkers: гость мог иметь в виду и «обязательно», и «хорошо бы».
	weakMarkers = []string{"важно", "нужен", "нужна", "нужно", "нужны", "надо"}

	resetWords = []string{"заново", "сначала", "сброс"}

	unitEuro   = []string{"евро", "eur", "euro"}
	unitStars  = []string{"звезды"} // «звезды» длиннее пяти букв: совпадёт по началу
	unitFloor  = []string{"этаж", "этажа", "этажи", "этажей", "этажу", "этаже"}
	unitMin    = []string{"мин", "минут", "минуты", "минуту", "минутах"}
	unitMeter  = []string{"м", "метр", "метра", "метров", "метре", "метры", "метру"}
	unitKm     = []string{"км", "километров"}
	ordSuffix  = []string{"го", "й", "ый", "ого", "ой", "ая"}
	ordinals   = map[string]int{"перв": 1, "втор": 2, "трет": 3, "четв": 4, "пят": 5, "шест": 6, "седь": 7, "восьм": 8, "девят": 9, "десят": 10}
	currentRef = []string{"этот", "этого", "него", "этом", "тот"}
	nameStops  = map[string]bool{"но": true, "только": true, "без": true, "с": true, "чтобы": true, "и": true}

	// Служебные слова: не значимые для поиска, в unmet_wishes из-за них ничего не пишется.
	stopWords = wordSet(`в во на с со у к от до по за из для и а но или же бы ли не ни да
		мне мой моя мою меня нам нас мы я вы ты он она оно они их его ее
		хочу хотим хотел хотела хотелось ищу ищем найди найти найдите подбери подберите покажи покажите подскажи
		посоветуй посоветуйте пожалуйста привет здравствуйте добрый день вечер утро спасибо
		отель отеля отеле отели отелей отелю гостиница гостиницу номер номера номере номеров номеру комната жить пожить
		остановиться поселиться снять забронировать бронь поездка
		это этот эта эти тот так там тут быть был была было будет есть как что очень самый самая более менее
		немного чуть просто еще тоже плюс вместе также где куда который которая которые
		рядом близко недалеко дальше ближе дольше больше поближе неподалеку около возле поблизости чтобы
		бюджет цена цены дешевле максимум не больше меньше менее
		ночь ночи сутки человек двоих двое трое пара пару паре
		барселона барселоне барселону город городе
		пожелание пожелания нужно надо нужен нужна нужны можно`)
)

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// anyMatches сообщает, подходит ли слово к одной из форм (правило длины основы — WordMatches).
func anyMatches(forms []string, w string) bool {
	return slices.ContainsFunc(forms, func(f string) bool { return WordMatches(f, w) })
}

type numeric struct {
	kind  string // price, stars, floor, minutes, meters
	value float64
	op    Op
	at    int // индекс числа в части
}

// part — часть сообщения (между знаками препинания и разделяющими словами).
type part struct {
	tokens   []string
	consumed []bool
}

func splitParts(text string) []*part {
	text = strings.ReplaceAll(strings.ToLower(text), "ё", "е")
	text = strings.ReplaceAll(text, "€", " евро ")
	text = digitLetter.ReplaceAllString(text, "$1 $2")
	text = letterDigit.ReplaceAllString(text, "$1 $2")
	var parts []*part
	for _, chunk := range clauseSep.Split(text, -1) {
		cur := &part{}
		flush := func() {
			if len(cur.tokens) > 0 {
				cur.consumed = make([]bool, len(cur.tokens))
				parts = append(parts, cur)
			}
			cur = &part{}
		}
		for _, w := range PhraseWords(chunk) {
			if clauseBreakWords[w] {
				flush()
				continue
			}
			cur.tokens = append(cur.tokens, w)
		}
		flush()
	}
	return parts
}

func (p *part) mark(from, to int) {
	for i := from; i < to && i < len(p.consumed); i++ {
		if i >= 0 {
			p.consumed[i] = true
		}
	}
}

func (p *part) prev(i, n int) string {
	if i-n < 0 {
		return ""
	}
	return p.tokens[i-n]
}

// Parse разбирает сообщение гостя.
func (l Lexicon) Parse(text string) ParseResult {
	var res ParseResult
	parts := splitParts(text)

	type found struct {
		part       int
		at         int
		key        string
		value      any
		hasValue   bool
		importance Importance
		weak       bool
	}
	var wishes []found
	var nums []numeric
	numPart := map[int]int{} // индекс в nums → индекс части
	type ambFound struct {
		amb        Ambiguity
		importance Importance
		weak       bool
	}
	var ambs []ambFound

	for pi, p := range parts {
		l.parseSimilar(p, &res) // остаток части разбирается дальше
		for i, w := range p.tokens {
			if slices.Contains(resetWords, w) {
				res.Reset = true
				p.mark(i, i+1)
			}
		}
		for _, n := range parseNumerics(p) {
			numPart[len(nums)] = pi
			nums = append(nums, n)
		}

		// Маркеры важности.
		imp, weak := Prefer, false
		for i, w := range p.tokens {
			if p.consumed[i] {
				continue
			}
			switch {
			case slices.Contains(strongMarkers, w):
				if p.prev(i, 1) == "не" {
					imp = Prefer
					p.mark(i-1, i+1)
				} else {
					imp = Must
					p.mark(i, i+1)
				}
			case slices.Contains(preferMarkers, w):
				p.mark(i, i+1)
			case slices.Contains(weakMarkers, w) && p.prev(i, 1) != "не":
				weak = true
				p.mark(i, i+1)
			}
		}
		if imp == Must {
			weak = false
		}

		// Фразы реестра: слева направо, длинные первыми.
		for i := 0; i < len(p.tokens); i++ {
			if p.consumed[i] {
				continue
			}
			e, n := l.matchAt(p, i)
			if e == nil {
				continue
			}
			from := i
			neg := false
			switch {
			case from > 0 && negators[p.prev(from, 1)]:
				neg, from = true, from-1
			case negVerbs[p.prev(from, 1)] && p.prev(from, 2) == "не" && from > 1:
				neg, from = true, from-2
			}
			if e.poi {
				if poiNearHours(p, i, n) {
					continue // «круглосуточное кафе», «бар, открытый ночью»: часы работы — не тип заведения
				}
				// Границы фразы считаются заново: общий разбор отрицания выше не знал о прилагательных типа.
				// «ночных клубов»: прилагательное входит во фразу и не попадает в неразобранное.
				from, neg = i, false
				for from > 0 && !p.consumed[from-1] && anyMatches(l.reg.poi.Fillers, p.tokens[from-1]) {
					from--
				}
				switch {
				case from > 0 && negators[p.prev(from, 1)]:
					neg, from = true, from-1
				case negVerbs[p.prev(from, 1)] && p.prev(from, 2) == "не" && from > 1:
					neg, from = true, from-2
				}
			}
			phrase := strings.Join(p.tokens[i:i+n], " ")
			p.mark(from, i+n)
			if e.amb != nil {
				res.Matches = append(res.Matches, Match{Phrase: phrase, Key: e.amb[0].Key, Negated: neg, Ambiguous: true})
				if neg {
					for _, o := range e.amb {
						res.Remove = appendUnique(res.Remove, o.Key)
					}
				} else {
					ambs = append(ambs, ambFound{Ambiguity{Phrase: phrase, Options: slices.Clone(e.amb)}, imp, weak})
				}
				i += n - 1
				continue
			}
			res.Matches = append(res.Matches, Match{Phrase: phrase, Key: e.key, Negated: neg})
			if neg {
				res.Remove = appendUnique(res.Remove, e.key)
			} else {
				wishes = append(wishes, found{part: pi, at: i, key: e.key, value: e.value, hasValue: e.hasValue, importance: imp, weak: weak})
			}
			i += n - 1
		}
	}

	// Желания: одно на ключ; значение из value_synonyms сильнее значения по умолчанию, важность — самая сильная.
	byKey := map[string]int{}
	var ordered []found
	for _, f := range wishes {
		if j, ok := byKey[f.key]; ok {
			o := &ordered[j]
			if f.hasValue && !o.hasValue {
				o.value, o.hasValue = f.value, true
			}
			if f.importance == Must {
				o.importance, o.weak = Must, false
			}
			o.weak = o.weak && f.weak
			continue
		}
		byKey[f.key] = len(ordered)
		ordered = append(ordered, f)
	}

	// Числа: цена и звёзды — фильтры, этаж — для желаний фасада, минуты и метры — для чисел.
	used := map[int]bool{}
	for ni, n := range nums {
		switch n.kind {
		case "price":
			res.Filters.PriceMax = int(n.value)
			used[ni] = true
		case "stars":
			res.Filters.StarsMin = int(n.value)
			used[ni] = true
		case "floor":
			res.MinFloor = int(n.value)
			used[ni] = true
		}
	}
	numberOf := map[string]float64{}
	numberOp := map[string]Op{}
	for ni, n := range nums {
		if n.kind != "minutes" && n.kind != "meters" {
			continue
		}
		best, bestDist := -1, 1<<30
		for oi, f := range ordered {
			c, ok := l.reg.Get(f.key)
			if !ok || c.ValueType != ValueNumber {
				continue
			}
			if _, taken := numberOf[f.key]; taken {
				continue
			}
			if IsNearPoiKey(f.key) && n.kind != "meters" {
				continue // «клуб в 10 минутах» не переводим: радиус заведения задаётся в метрах
			}
			d := abs(f.at - n.at)
			if f.part != numPart[ni] {
				d += 1000
			}
			if d < bestDist {
				best, bestDist = oi, d
			}
		}
		if best < 0 {
			res.Notes = append(res.Notes, fmt.Sprintf("число %s без характеристики, которой оно относится — не учтено", formatNumber(n.value)))
			continue
		}
		f := ordered[best]
		c, _ := l.reg.Get(f.key)
		v := n.value
		if IsNearPoiKey(f.key) {
			if v > PoiRadiusM {
				res.Notes = append(res.Notes, fmt.Sprintf("заведения известны только в радиусе %s м, беру %s м", formatNumber(PoiRadiusM), formatNumber(PoiRadiusM)))
				v = PoiRadiusM
			}
		} else if n.kind == "meters" {
			speed := strategyParam(c, "speed_m_per_min")
			if speed <= 0 {
				res.Notes = append(res.Notes, fmt.Sprintf("расстояние %s м не переведено в минуты для «%s» — не учтено", formatNumber(n.value), c.Title))
				continue
			}
			v = math.Ceil(n.value / speed)
			res.Notes = append(res.Notes, fmt.Sprintf("%s м пешком считаю ≈ %s мин (%s м/мин)", formatNumber(n.value), formatNumber(v), formatNumber(speed)))
		}
		numberOf[f.key], numberOp[f.key] = v, n.op
		used[ni] = true
	}

	for _, f := range ordered {
		c, _ := l.reg.Get(f.key)
		d := DefaultDesire(c)
		if f.hasValue {
			d.Value = f.value
		}
		if v, ok := numberOf[f.key]; ok {
			d.Value, d.Op = v, numberOp[f.key]
		}
		d.Importance = f.importance
		if CategoryLevel(c) {
			d.MinFloor = res.MinFloor
		}
		if f.weak {
			res.WeakKeys = append(res.WeakKeys, f.key)
		}
		res.Wishes = append(res.Wishes, d)
	}
	if res.MinFloor > 0 && !slices.ContainsFunc(res.Wishes, func(d Desire) bool { return d.MinFloor > 0 }) &&
		len(res.Ambiguities) == 0 && len(ambs) == 0 {
		res.Notes = append(res.Notes, "этаж без вида или тишины, к которым он относится, — запомнил, применю к ним, когда они появятся")
	}

	// Неоднозначность, которую гость уже снял явным названием, не нужна («у моря, с видом на море»).
	for _, a := range ambs {
		resolved := false
		for _, o := range a.amb.Options {
			if _, ok := byKey[o.Key]; ok {
				resolved = true
			}
		}
		if resolved {
			continue
		}
		for i := range a.amb.Options {
			a.amb.Options[i].Desire.Importance = a.importance
			if c, ok := l.reg.Get(a.amb.Options[i].Key); ok && CategoryLevel(c) {
				a.amb.Options[i].Desire.MinFloor = res.MinFloor
			}
		}
		res.Ambiguities = append(res.Ambiguities, a.amb)
	}

	// Проверка по схеме реестра: что не проходит — не желание, а допущение для гостя.
	valid := res.Wishes[:0]
	for _, d := range res.Wishes {
		if _, _, err := NormalizeWish(l.reg, d); err != nil {
			res.Notes = append(res.Notes, "желание пропущено: "+err.Error())
			continue
		}
		valid = append(valid, d)
	}
	res.Wishes = valid

	res.Unmet = leftovers(parts)
	return res
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func appendUnique(ss []string, s string) []string {
	if slices.Contains(ss, s) {
		return ss
	}
	return append(ss, s)
}

func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// strategyParam — числовой параметр из первой стратегии характеристики, где он есть.
func strategyParam(c Characteristic, name string) float64 {
	for _, s := range c.Strategies {
		if v, ok := s.Params[name]; ok {
			return v
		}
	}
	return 0
}

// matchAt ищет фразу словаря, которая начинается с токена i и целиком лежит на неиспользованных токенах.
func (l Lexicon) matchAt(p *part, i int) (*lexEntry, int) {
	if e, n := matchEntries(l.entries, p, i); e != nil {
		return e, n
	}
	return matchEntries(l.poiEntries, p, i)
}

func matchEntries(entries []lexEntry, p *part, i int) (*lexEntry, int) {
	for ei := range entries {
		e := &entries[ei]
		if i+len(e.words) > len(p.tokens) {
			continue
		}
		ok := true
		for k, w := range e.words {
			if p.consumed[i+k] || !WordMatches(w, p.tokens[i+k]) {
				ok = false
				break
			}
		}
		if ok {
			return e, len(e.words)
		}
	}
	return nil, 0
}

// hoursWords — слова о часах работы: фраза с ними не про тип заведения, а про «открыто ночью» (характеристики реестра).
var hoursWords = []string{"круглосуточный", "открытый", "работающий"}

// poiNearHours сообщает, что слева или справа от фразы типа заведения стоит слово о часах работы.
func poiNearHours(p *part, i, n int) bool {
	for _, j := range []int{i - 1, i + n} {
		if j >= 0 && j < len(p.tokens) && anyMatches(hoursWords, p.tokens[j]) {
			return true
		}
	}
	return false
}

// parseNumerics находит числа с единицами («до 150 €», «с 4-го этажа», «10 минут», «500 метров», «4 звезды») и помечает их.
func parseNumerics(p *part) []numeric {
	var out []numeric
	for i := 0; i < len(p.tokens); i++ {
		if p.consumed[i] || !digitsWord.MatchString(p.tokens[i]) {
			continue
		}
		v, _ := strconv.ParseFloat(p.tokens[i], 64)
		j := i + 1
		for j < len(p.tokens) && slices.Contains(ordSuffix, p.tokens[j]) {
			j++
		}
		unit := ""
		if j < len(p.tokens) {
			unit = p.tokens[j]
		}
		n := numeric{value: v, at: i}
		end := j + 1
		switch {
		case unit != "" && anyMatches(unitEuro, unit):
			n.kind = "price"
			if p.prev(i, 1) == "от" {
				continue
			}
		case unit != "" && anyMatches(unitStars, unit):
			n.kind = "stars"
		case unit != "" && anyMatches(unitFloor, unit):
			n.kind = "floor"
		case unit != "" && anyMatches(unitMin, unit):
			n.kind = "minutes"
		case unit != "" && anyMatches(unitKm, unit):
			n.kind, n.value = "meters", v*1000
		case unit != "" && anyMatches(unitMeter, unit):
			n.kind = "meters"
		case i > 0 && anyMatches(unitFloor, p.tokens[i-1]):
			n.kind, end = "floor", i+1 // «этаж 4»
		case p.prev(i, 1) == "бюджет":
			n.kind, end = "price", i+1
		default:
			continue
		}
		switch {
		case p.prev(i, 1) == "от" || (p.prev(i, 2) == "не" && (p.prev(i, 1) == "менее" || p.prev(i, 1) == "меньше")):
			n.op = OpGe
		}
		from := i
		if q := p.prev(i, 1); q == "до" || q == "от" || q == "с" || q == "бюджет" {
			from = i - 1
		}
		if i > 0 && anyMatches(unitFloor, p.tokens[i-1]) {
			from = i - 1
		}
		p.mark(from, end)
		out = append(out, n)
		i = end - 1
	}
	return out
}

// parseSimilar разбирает «похожий на второй / на этот / на Hotel Arts» и помечает использованные токены.
func (l Lexicon) parseSimilar(p *part, res *ParseResult) {
	for i, w := range p.tokens {
		if p.consumed[i] || !(WordMatches("похожий", w) || w == "аналог" || w == "аналогичный") {
			continue
		}
		j := i + 1
		for j < len(p.tokens) && (p.tokens[j] == "на" || p.tokens[j] == "как") {
			j++
		}
		ref := &SimilarRef{}
		switch {
		case j >= len(p.tokens):
			ref.Current = true
		case anyMatches(currentRef, p.tokens[j]):
			ref.Current = true
			j++
			if j < len(p.tokens) && (p.tokens[j] == "отель" || p.tokens[j] == "отеля") {
				j++
			}
		case digitsWord.MatchString(p.tokens[j]):
			n, _ := strconv.Atoi(p.tokens[j])
			ref.Ordinal = n
			j++
			for j < len(p.tokens) && slices.Contains(ordSuffix, p.tokens[j]) {
				j++
			}
		default:
			if n := ordinalOf(p.tokens[j]); n > 0 {
				ref.Ordinal = n
				j++
				break
			}
			var name []string
			for ; j < len(p.tokens) && !nameStops[p.tokens[j]]; j++ {
				name = append(name, p.tokens[j])
			}
			ref.Name = strings.Join(name, " ")
		}
		p.mark(i, j)
		res.Similar = ref
		return
	}
}

func ordinalOf(w string) int {
	for stem, n := range ordinals {
		if strings.HasPrefix(w, stem) {
			return n
		}
	}
	return 0
}

// leftovers собирает куски частей, не понятые ни одной фразой: подряд идущие неиспользованные слова,
// без служебных слов по краям. Это кандидаты в unmet_wishes.
func leftovers(parts []*part) []string {
	var out []string
	for _, p := range parts {
		var run []string
		flush := func() {
			for len(run) > 0 && stopWords[run[0]] {
				run = run[1:]
			}
			for len(run) > 0 && stopWords[run[len(run)-1]] {
				run = run[:len(run)-1]
			}
			if len(run) > 0 {
				out = append(out, strings.Join(run, " "))
			}
			run = nil
		}
		for i, w := range p.tokens {
			if p.consumed[i] || digitsWord.MatchString(w) {
				flush()
				continue
			}
			run = append(run, w)
		}
		flush()
	}
	return out
}

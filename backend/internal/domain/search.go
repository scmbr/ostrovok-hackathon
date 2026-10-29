package domain

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Поиск по структурному запросу: желания гостя сопоставляются с фактами, для каждого отеля выбирается лучший
// вариант номера (категория + пожелание + честная вероятность). Формула и примеры — docs/search.md.

// Важность желания.
type Importance string

const (
	Must   Importance = "must"
	Prefer Importance = "prefer"
)

// Op — способ сравнения значения желания с фактом.
type Op string

const (
	OpEq Op = "eq" // равно
	OpLe Op = "le" // не больше (число) или не дальше от лучшего конца шкалы (ordered enum, значение «или лучше»)
	OpGe Op = "ge" // не меньше
)

// Константы оценки (docs/search.md).
const (
	// ScorePrior — вероятность выполнения желания, когда уверенности в факте нет.
	ScorePrior = 0.5
	// UnknownScore — оценка желания, по которому данных нет: ScorePrior минус небольшой штраф. Неизвестное не равно «нет».
	UnknownScore = 0.4
	// MustMinScore — must с оценкой ниже порога считается нарушенным.
	MustMinScore = 0.5
	// MatchProbability — с этой вероятностью желание считается выполненным («совпало»).
	MatchProbability = 0.95
	// MinConfidence — ниже этой уверенности факта совпадение остаётся «под вопросом».
	MinConfidence = 0.6
	// NoFactConfidence — уверенность, когда у категории нет итогового факта.
	NoFactConfidence = 0.5
	// ViolationProbability — ниже этой вероятности известное значение нарушает желание («не совпало»).
	ViolationProbability = 0.05
	// WishFulfilProbability — вероятность, что отель выполнит пожелание к брони, пока он его не подтвердил (мок).
	WishFulfilProbability = 0.7
	// WishRefusedProbability — то же, если отельер ответил, что пожелания не гарантирует (wish_guarantee = false).
	WishRefusedProbability = 0.4
	// WishGuaranteedProbability — то же, если отельер сообщил, что гарантирует пожелания (wish_guarantee).
	WishGuaranteedProbability = 0.95
	// WishGuaranteeKey — характеристика «отель гарантирует пожелание к номеру», только от отельера.
	WishGuaranteeKey = "wish_guarantee"

	WeightMust   = 2.0
	WeightPrefer = 1.0
	// ProfileWeightMust и ProfileWeightPrefer — веса характеристик желаний в расстоянии «похожего».
	ProfileWeightMust   = 3.0
	ProfileWeightPrefer = 2.0
	// profileRangeScaleFloors — сколько этажей разницы в начале вида считается «совсем другим».
	profileRangeScaleFloors = 4.0
	// DefaultSearchLimit и MaxSearchLimit — сколько отелей отдаёт поиск.
	DefaultSearchLimit = 10
	MaxSearchLimit     = 50
)

// Desire — желание гостя. Value: bool, строка (enum) или число; пусто — значение по умолчанию из реестра.
type Desire struct {
	Key        string     `json:"key"`
	Op         Op         `json:"op,omitempty"`
	Value      any        `json:"value,omitempty"`
	Importance Importance `json:"importance,omitempty"`
	// MinFloor — номер нужен не ниже этого этажа (только для характеристик фасада).
	MinFloor int `json:"min_floor,omitempty"`
}

// Filters — базовые фильтры карточки (мок).
type Filters struct {
	PriceMax int `json:"price_max,omitempty"` // евро за ночь, цена варианта номера
	StarsMin int `json:"stars_min,omitempty"`
}

// Query — структурный запрос гостя.
type Query struct {
	Wishes  []Desire `json:"wishes"`
	Filters Filters  `json:"filters"`
	// SessionID — сессия поиска: спрос по ключу желания растёт один раз за сессию.
	SessionID string `json:"session_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	// NoDemand — пробный поиск (например, при выборе уточнения): спрос не пишется. Из JSON поле не читается.
	NoDemand bool `json:"-"`
}

// CategoryLevel сообщает, читается ли желание по категориям номеров (характеристика фасада с блоком category).
func CategoryLevel(c Characteristic) bool { return c.Level == LevelFacade && c.Category != nil }

// Searchable сообщает, можно ли искать по характеристике.
func Searchable(c Characteristic) bool {
	if CategoryLevel(c) {
		return true
	}
	if c.Level != LevelHotel {
		return false
	}
	switch c.ValueType {
	case ValueBool, ValueEnum, ValueNumber:
		return true
	}
	return false
}

// NormalizeWish проверяет желание по схеме характеристики из реестра и подставляет значения по умолчанию.
func NormalizeWish(reg Registry, w Desire) (Desire, Characteristic, error) {
	c, ok := reg.Get(w.Key)
	if !ok {
		return w, c, fmt.Errorf("характеристики %q нет в реестре", w.Key)
	}
	if !Searchable(c) {
		return w, c, fmt.Errorf("по характеристике %q искать нельзя (уровень %s, тип %s)", w.Key, c.Level, c.ValueType)
	}
	switch w.Importance {
	case "":
		w.Importance = Prefer
	case Must, Prefer:
	default:
		return w, c, fmt.Errorf("%s: важность %q неизвестна (must | prefer)", w.Key, w.Importance)
	}
	if w.MinFloor < 0 {
		return w, c, fmt.Errorf("%s: этаж не может быть отрицательным", w.Key)
	}
	fail := func(f string, a ...any) (Desire, Characteristic, error) {
		return w, c, fmt.Errorf("%s: %s", w.Key, fmt.Sprintf(f, a...))
	}
	if CategoryLevel(c) {
		r := c.Category
		if w.Op != "" && w.Op != OpEq {
			return fail("для категорий номеров поддержано только сравнение eq")
		}
		w.Op = OpEq
		if w.Value == nil {
			w.Value = r.Target
		}
		v, ok := w.Value.(string)
		if !ok || !slices.Contains(r.Values, v) {
			return fail("значение %v не из %v", w.Value, r.Values)
		}
		return w, c, nil
	}
	if w.MinFloor != 0 {
		return fail("этаж задаётся только для характеристик фасада")
	}
	switch c.ValueType {
	case ValueBool:
		if w.Value == nil {
			w.Value = true
		}
		if _, ok := w.Value.(bool); !ok {
			return fail("значение должно быть true или false")
		}
		if w.Op != "" && w.Op != OpEq {
			return fail("для bool поддержано только eq")
		}
		w.Op = OpEq
	case ValueEnum:
		v, ok := w.Value.(string)
		if !ok || !slices.Contains(c.Values, v) {
			return fail("значение %v не из %v", w.Value, c.Values)
		}
		switch {
		case !c.Ordered:
			if w.Op != "" && w.Op != OpEq {
				return fail("шкала не упорядочена, поддержано только eq")
			}
			w.Op = OpEq
		case w.Op == "":
			w.Op = betterDirection(c)
		case w.Op != OpEq && w.Op != OpLe && w.Op != OpGe:
			return fail("сравнение %q неизвестно", w.Op)
		}
	case ValueNumber:
		if w.Value == nil && IsNearPoiKey(w.Key) {
			w.Value = c.DefaultNumber // радиус не назван — весь радиус данных
		}
		v, ok := toFloat(w.Value)
		if !ok {
			return fail("значение должно быть числом")
		}
		if IsNearPoiKey(w.Key) {
			// Данные о заведениях есть только в радиусе PoiRadiusM; сравнение только «не дальше».
			if v <= 0 || (w.Op != "" && w.Op != OpLe) {
				return fail("радиус должен быть положительным, сравнение — только le")
			}
			v = math.Min(v, PoiRadiusM)
		}
		w.Value = v
		if w.Op == "" {
			w.Op = OpLe
		}
		if w.Op != OpLe && w.Op != OpGe {
			return fail("для чисел поддержаны le и ge")
		}
	}
	return w, c, nil
}

// betterDirection — в какую сторону шкалы лучше: le (к началу) или ge (к концу), по выгодным значениям.
func betterDirection(c Characteristic) Op {
	if len(c.Values) > 0 && slices.Contains(c.FavorableValues, c.Values[len(c.Values)-1]) &&
		!slices.Contains(c.FavorableValues, c.Values[0]) {
		return OpGe
	}
	return OpLe
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func toBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(x)
		return b, err == nil
	}
	return false, false
}

// Status — как желание выполняется в варианте.
type Status string

const (
	StatusMatch     Status = "match"     // совпало
	StatusChance    Status = "chance"    // шанс N %
	StatusUncertain Status = "uncertain" // под вопросом: низкая уверенность, конфликт, устарело
	StatusNear      Status = "near"      // число на границе порога
	StatusMismatch  Status = "mismatch"  // не совпало
	StatusUnknown   Status = "unknown"   // данных нет — спросим отель
)

// EvidenceSource — свидетельство, из которого сложен факт, для объяснения «откуда известно».
type EvidenceSource struct {
	ID         int64
	Source     string
	AuthorType string
	AuthorID   string
	// AuthorTrust — репутация автора; ниже LowTrust объяснение помечает «со слов отеля».
	AuthorTrust  float64
	Reliability  float64
	Contribution float64
	ObservedAt   time.Time
	Claimed      map[string]any
}

// VariantWish — пожелание к брони, которое повышает шанс на нужный номер («восточная сторона, этаж 4+»).
type VariantWish struct {
	FacadeID   int64
	AzimuthDeg float64
	MinFloor   int // 0 — этаж не важен
	Rooms      float64
	Share      float64 // доля номеров категории под пожеланием
	// Probability — вероятность получить нужное, если забронировать с пожеланием: охват подходящих номеров × вероятность выполнения.
	Probability float64
	// HotelConfirmed — отельер сообщил, что гарантирует пожелания (wish_guarantee).
	HotelConfirmed bool
	// HotelRefused — отельер ответил, что пожелания не гарантирует.
	HotelRefused bool
}

// WishEval — оценка одного желания в варианте номера. Структура, а не текст: текст строится из неё.
type WishEval struct {
	Key        string
	Importance Importance
	Status     Status
	// PBase — вероятность без пожелания (доля подходящих номеров); PWish — с лучшим пожеланием, 0 — пожелания нет.
	PBase, PWish float64
	// P — вероятность, которая идёт в оценку: max(PBase, PWish) или 0/0,5/1 у характеристик отеля.
	P     float64
	Score float64
	// Value — найденное значение (число, строка, bool); nil, если данных нет.
	Value      any
	Confidence float64
	Conflict   string
	Caveat     string
	Wish       *VariantWish
	// Nearby — ближайшее заведение для желания «рядом с …» (near_poi.*); nil у остальных желаний и если ничего не найдено.
	Nearby *NearbyPoi
	// Guarantee — у категории подходят все номера.
	Guarantee  bool
	FactID     int64
	ObservedAt time.Time
	ValidUntil *time.Time
	Sources    []EvidenceSource
}

// Violated — нарушено ли must-желание: известное несовпадение или шанс ниже порога даже с пожеланием.
// Неизвестное и «на границе» нарушением не считаются.
func (e WishEval) Violated() bool {
	if e.Importance != Must {
		return false
	}
	return e.Status == StatusMismatch || (e.Status == StatusChance && e.Score < MustMinScore)
}

// Weight — вес желания в оценке.
func (i Importance) Weight() float64 {
	if i == Must {
		return WeightMust
	}
	return WeightPrefer
}

// WishScore — оценка желания: c·p + (1−c)·ScorePrior, где c — уверенность факта, p — вероятность выполнения.
func WishScore(p, confidence float64) float64 {
	c := math.Min(math.Max(confidence, 0), 1)
	return c*p + (1-c)*ScorePrior
}

func unknownEval(w Desire, caveat string) WishEval {
	return WishEval{Key: w.Key, Importance: w.Importance, Status: StatusUnknown, Score: UnknownScore, Caveat: caveat}
}

// EvalHotelWish оценивает желание по факту уровня отеля. f == nil или нет значения — неизвестно.
func EvalHotelWish(c Characteristic, w Desire, f *Fact) WishEval {
	if f == nil {
		return unknownEval(w, "данных нет — спросим отель")
	}
	raw, ok := f.Value[c.FieldName()]
	if !ok || raw == nil {
		return unknownEval(w, "данных нет — спросим отель")
	}
	e := WishEval{Key: w.Key, Importance: w.Importance, Value: raw, Confidence: f.Confidence,
		FactID: f.ID, ObservedAt: f.ObservedAt, ValidUntil: f.ValidUntil}
	sat, near, numP := false, false, 0.0
	switch c.ValueType {
	case ValueBool:
		got, ok := toBool(raw)
		want, _ := toBool(w.Value)
		if !ok {
			return unknownEval(w, "значение факта не разобрано")
		}
		sat = got == want
	case ValueEnum:
		got, ok := raw.(string)
		if !ok {
			return unknownEval(w, "значение факта не разобрано")
		}
		sat = enumSatisfies(c, w, got)
	case ValueNumber:
		v, ok := toFloat(raw)
		t, _ := toFloat(w.Value)
		if !ok {
			return unknownEval(w, "значение факта не разобрано")
		}
		e.Value = v
		numP = NumberProbability(c, w.Op, v, t)
		sat = numP >= MatchProbability
		near = numP >= ViolationProbability && !sat
		if near {
			e.Caveat = fmt.Sprintf("≈ %s%s, на границе", formatNum(v), unitSuffix(c))
		}
	}
	if f.Conflict != nil {
		e.Conflict = f.Conflict.Description
	}
	doubtful := f.Conflict != nil || f.WeakDisagreement || f.Stale || lowConfidence(f.Confidence)
	switch {
	case near:
		e.Status, e.P = StatusNear, numP
	case sat:
		e.P = 1
		e.Status = StatusMatch
		if doubtful {
			e.Status = StatusUncertain
		}
	default:
		e.Status = StatusMismatch
		if f.Conflict != nil || lowConfidence(f.Confidence) {
			e.Status = StatusUncertain
		}
	}
	e.PBase = e.P
	e.Score = WishScore(e.P, f.Confidence)
	if c.ValueType == ValueBool || c.ValueType == ValueEnum {
		e.Score = discreteScore(c, w, raw, sat, f.Confidence)
	}
	if e.Status == StatusUncertain && e.Caveat == "" {
		e.Caveat = doubtCaveat(f)
	}
	return e
}

// discreteScore — оценка желания по факту bool или enum. Уверенность такого факта уже вероятность его значения,
// поэтому оценка — вероятность того, что настоящее значение подходит: c, если значение факта подходит; иначе
// остаток 1 − c делится поровну между остальными значениями шкалы (у bool оно одно), и берутся подходящие.
// Формула c·p + (1 − c)·0,5 здесь завышала: «да» с c = 0,5 давало 0,75 вместо 0,5.
func discreteScore(c Characteristic, w Desire, raw any, sat bool, conf float64) float64 {
	conf = math.Min(math.Max(conf, 0), 1)
	if sat {
		return conf
	}
	k := 2
	suitable := 1
	if c.ValueType == ValueEnum && len(c.Values) > 1 {
		k = len(c.Values)
		suitable = 0
		got, _ := raw.(string)
		for _, v := range c.Values {
			if v != got && enumSatisfies(c, w, v) {
				suitable++
			}
		}
	}
	return math.Min(1, float64(suitable)*(1-conf)/float64(k-1))
}

func doubtCaveat(f *Fact) string {
	switch {
	case f.Conflict != nil:
		return "источники спорят: " + f.Conflict.Description
	case f.Stale:
		return "данные устарели, ждут пересчёта"
	case f.WeakDisagreement:
		return "есть расхождение источников"
	}
	return fmt.Sprintf("уверенность %.0f %%", f.Confidence*100)
}

func formatNum(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', -1, 64), ".", ",", 1)
}

func unitSuffix(c Characteristic) string {
	switch c.FieldName() {
	case "minutes":
		return " мин"
	case PoiFieldName:
		return " м"
	}
	return ""
}

// nearBand — полоса «на границе» вокруг порога желания: significant_diff, но не шире половины самого порога
// (иначе при пороге 5 минут и significant_diff 5 «на границе» было бы всё от 0 до 10 минут).
func nearBand(c Characteristic, threshold float64) float64 {
	if threshold > 0 {
		return math.Min(c.SignificantDiff, threshold/2)
	}
	return c.SignificantDiff
}

// NumberProbability — вероятность, что число v выполняет желание «v ≤ t» (OpLe) или «v ≥ t» (OpGe). Внутри полосы
// вокруг порога вероятность плавная: p = clamp(0,5 + (t − v) / (2·полоса), 0, 1) для le (для ge разность с обратным знаком),
// на пороге 0,5, на краю полосы изнутри 1, снаружи 0. Без significant_diff — жёсткое 0 или 1.
func NumberProbability(c Characteristic, op Op, v, t float64) float64 {
	d := t - v
	if op == OpGe {
		d = v - t
	}
	band := nearBand(c, t)
	if band <= 0 {
		if d >= 0 {
			return 1
		}
		return 0
	}
	return math.Min(1, math.Max(0, 0.5+d/(2*band)))
}

// enumSatisfies — подходит ли значение got под желание: eq или «не хуже» по упорядоченной шкале.
func enumSatisfies(c Characteristic, w Desire, got string) bool {
	want, _ := w.Value.(string)
	if w.Op == OpEq || !c.Ordered {
		return got == want
	}
	gi, wi := slices.Index(c.Values, got), slices.Index(c.Values, want)
	if gi < 0 || wi < 0 {
		return false
	}
	if w.Op == OpLe {
		return gi <= wi
	}
	return gi >= wi
}

// CategoryWishInput — данные категории номеров для оценки желания фасадной характеристики.
type CategoryWishInput struct {
	Assessment CategoryAssessment
	// Fact — итоговый факт категории (движок правды); nil — нет.
	Fact *Fact
	// WishGuaranteed — отельер сообщил, что гарантирует пожелания.
	WishGuaranteed bool
	// WishRefused — отельер сообщил, что не гарантирует пожелания.
	WishRefused bool
	// AzimuthOf — азимут фасада по id для пожелания.
	AzimuthOf func(facadeID int64) float64
}

// EvalCategoryWish оценивает желание по категории номеров. Вероятность без пожелания — доля подходящих номеров;
// с пожеланием — охват подходящих номеров пожеланием × вероятность, что отель его выполнит. В оценку идёт лучшая из двух.
// Название категории вероятность не повышает: оно лишь утверждение отеля, проверяет его расчёт.
func EvalCategoryWish(c Characteristic, w Desire, in CategoryWishInput) WishEval {
	a := in.Assessment
	if a.Rooms == 0 || a.Unknown >= 1-1e-9 {
		return unknownEval(w, "размещение или факты фасадов неизвестны — спросим отель")
	}
	want, _ := w.Value.(string)
	rule := c.Category
	conf := NoFactConfidence
	e := WishEval{Key: w.Key, Importance: w.Importance, Value: dominantValue(rule.Values, a.Shares), Guarantee: a.Guarantee}
	pBase := a.Probability
	var factValue string
	if f := in.Fact; f != nil {
		conf = f.Confidence
		e.FactID, e.ObservedAt, e.ValidUntil = f.ID, f.ObservedAt, f.ValidUntil
		factValue, _ = f.Value[rule.ValueField].(string)
		if factValue != "" {
			e.Value = factValue
		}
		if f.Conflict != nil {
			e.Conflict = f.Conflict.Description
		}
	}
	e.Confidence = conf
	// Итог движка правды противоречит расчёту (гости или отельер против преобладающего значения): расчёту не верим.
	disagree := in.Fact != nil && factValue != "" && factValue != want && dominantValue(rule.Values, a.Shares) == want
	if disagree {
		pBase *= 1 - conf
		e.Guarantee = false
	}
	e.PBase = pBase
	if !e.Guarantee && !disagree && a.Wish != nil && pBase > 0 {
		cover := math.Min(1, a.Wish.Rooms/(pBase*float64(a.Rooms)))
		fulfil := WishFulfilProbability
		switch {
		case in.WishGuaranteed:
			fulfil = WishGuaranteedProbability
		case in.WishRefused:
			fulfil = WishRefusedProbability
		}
		vw := &VariantWish{FacadeID: a.Wish.FacadeID, MinFloor: a.Wish.MinFloor, Rooms: a.Wish.Rooms, Share: a.Wish.Share,
			Probability: cover * fulfil, HotelConfirmed: in.WishGuaranteed, HotelRefused: in.WishRefused && !in.WishGuaranteed}
		if in.AzimuthOf != nil {
			vw.AzimuthDeg = in.AzimuthOf(a.Wish.FacadeID)
		}
		e.Wish, e.PWish = vw, vw.Probability
	}
	e.P = math.Max(e.PBase, e.PWish)
	e.Score = WishScore(e.P, conf)
	weak := in.Fact != nil && (in.Fact.WeakDisagreement || in.Fact.Stale)
	switch {
	case e.P == 0 && !lowConfidence(conf):
		e.Status = StatusMismatch
	case e.P == 0, disagree, e.Conflict != "":
		e.Status = StatusUncertain
	case e.Guarantee && !lowConfidence(conf) && !weak:
		e.Status = StatusMatch
	case e.Guarantee || lowConfidence(conf) || weak:
		e.Status = StatusUncertain
	default:
		e.Status = StatusChance
	}
	switch {
	case e.Status == StatusMismatch && e.Conflict != "":
		e.Caveat = "название категории или гости спорят с расчётом: " + e.Conflict
	case e.Conflict != "":
		e.Caveat = "источники спорят: " + e.Conflict
	case e.Wish != nil:
		e.Caveat = fmt.Sprintf("гарантии нет: %.0f %% номеров без пожелания; с пожеланием — если отель подтвердит", e.PBase*100)
	case in.Fact != nil && weak:
		e.Caveat = doubtCaveat(in.Fact)
	}
	return e
}

// dominantValue — значение с наибольшей долей номеров; при равенстве — первое по порядку values.
func dominantValue(values []string, shares map[string]float64) string {
	best, bestShare := "", 0.0
	for _, v := range values {
		if s := shares[v]; s > bestShare+1e-9 {
			best, bestShare = v, s
		}
	}
	return best
}

// Варианты номера.

// Variant — вариант номера для запроса: категория, цена и оценки всех желаний.
type Variant struct {
	CategoryID int64
	Name       string
	Rooms      int
	PriceEUR   int
	Evals      []WishEval
	Score      float64
	// Violations и Unknown — ключи нарушенных must и желаний без данных.
	Violations []string
	Unknown    []string
	// Guaranteed — все желания варианта выполнены: у категорийных есть гарантия, остальные совпали.
	// «Под вопросом», «шанс» и «данных нет» по любому желанию снимают признак.
	Guaranteed bool
}

// NewVariant собирает вариант из оценок желаний.
func NewVariant(categoryID int64, name string, rooms, price int, evals []WishEval, categoryKeys map[string]bool) Variant {
	v := Variant{CategoryID: categoryID, Name: name, Rooms: rooms, PriceEUR: price, Evals: evals}
	sum, weight := 0.0, 0.0
	allSure := len(evals) > 0
	for _, e := range evals {
		w := e.Importance.Weight()
		sum += w * e.Score
		weight += w
		if e.Violated() {
			v.Violations = append(v.Violations, e.Key)
		}
		if e.Status == StatusUnknown {
			v.Unknown = append(v.Unknown, e.Key)
		}
		if e.Status != StatusMatch || (categoryKeys[e.Key] && !e.Guarantee) {
			allSure = false
		}
	}
	if weight > 0 {
		v.Score = sum / weight
	}
	v.Guaranteed = allSure
	return v
}

// better — вариант a лучше b: меньше нарушений, выше оценка, ниже цена.
func better(a, b Variant) bool {
	if len(a.Violations) != len(b.Violations) {
		return len(a.Violations) < len(b.Violations)
	}
	if math.Abs(a.Score-b.Score) > 1e-9 {
		return a.Score > b.Score
	}
	if a.PriceEUR != b.PriceEUR {
		return a.PriceEUR < b.PriceEUR
	}
	return a.CategoryID < b.CategoryID
}

// ChooseVariants выбирает лучший вариант и одну альтернативу: дешевле лучшего или дающую гарантию, которой у лучшего нет.
// Альтернатива не должна нарушать больше желаний, чем лучший вариант.
func ChooseVariants(vs []Variant) (best Variant, alt *Variant, ok bool) {
	if len(vs) == 0 {
		return best, nil, false
	}
	sorted := slices.Clone(vs)
	sort.SliceStable(sorted, func(i, j int) bool { return better(sorted[i], sorted[j]) })
	best = sorted[0]
	for i := 1; i < len(sorted); i++ {
		v := sorted[i]
		if len(v.Violations) > len(best.Violations) {
			continue
		}
		if v.PriceEUR < best.PriceEUR || (v.Guaranteed && !best.Guaranteed) {
			alt = &sorted[i]
			break
		}
	}
	return best, alt, true
}

// Цена категории.

// categoryPriceFactor — множитель цены карточки по названию категории (мок: у карточки одна цена на отель).
var categoryPriceFactor = map[string]float64{
	"standard": 1.0, "superior": 1.2, "deluxe": 1.45, "family": 1.5, "suite": 2.0,
}

// seaViewPriceFactor — надбавка категории, в названии которой обещан вид на море.
const seaViewPriceFactor = 1.3

// CategoryPriceEUR — цена категории за ночь: цена карточки × множитель по названию. Мок.
func CategoryPriceEUR(card Card, name string) int {
	f := 1.0
	if TitleClaimsSeaView(name) {
		f = seaViewPriceFactor
	} else if m, ok := categoryPriceFactor[strings.ToLower(strings.TrimSpace(name))]; ok {
		f = m
	}
	return int(math.Round(float64(card.PriceEUR) * f))
}

// Выдача.

// HotelResult — отель в выдаче.
type HotelResult struct {
	HotelID  int64
	Name     string
	District string
	Card     Card
	Best     Variant
	Alt      *Variant
	// OtherConflicts — открытые конфликты в других категориях отеля по желаниям запроса (например, «Sea View»,
	// чьё название спорит с расчётом): гость должен о них знать, даже если лучший вариант — другая категория.
	OtherConflicts []VariantConflict
}

// VariantConflict — конфликт по желанию в категории номеров.
type VariantConflict struct {
	Category    string
	Key         string
	Description string
}

// ConflictsOutside собирает конфликты вариантов, кроме лучшего.
func ConflictsOutside(vs []Variant, best int64) []VariantConflict {
	var out []VariantConflict
	for _, v := range vs {
		if v.CategoryID == best {
			continue
		}
		for _, e := range v.Evals {
			if e.Conflict != "" {
				out = append(out, VariantConflict{Category: v.Name, Key: e.Key, Description: e.Conflict})
			}
		}
	}
	return out
}

// Violations — нарушенные must лучшего варианта.
func (h HotelResult) Violations() []string { return h.Best.Violations }

// RankHotels упорядочивает выдачу: сначала без нарушений must, затем по оценке, цене, id. Нарушители идут в конце.
func RankHotels(hs []HotelResult) {
	sort.SliceStable(hs, func(i, j int) bool {
		a, b := hs[i], hs[j]
		if (len(a.Best.Violations) == 0) != (len(b.Best.Violations) == 0) {
			return len(a.Best.Violations) == 0
		}
		if math.Abs(a.Best.Score-b.Best.Score) > 1e-9 {
			return a.Best.Score > b.Best.Score
		}
		if a.Best.PriceEUR != b.Best.PriceEUR {
			return a.Best.PriceEUR < b.Best.PriceEUR
		}
		return a.HotelID < b.HotelID
	})
}

// Профиль и «похожий».

// ProfileValue — значение характеристики в профиле отеля.
type ProfileValue struct {
	Num  float64 // число, индекс ступени, 0/1 для bool; для range — этаж
	Str  string  // значение неупорядоченного enum
	None bool    // range без значения (вида нет)
}

// Profile — вектор фактов отеля по характеристикам реестра: ключ → значение. Без звёзд и цены.
type Profile map[string]ProfileValue

// BuildProfile строит профиль по фактам уровня отеля; характеристики района и неразобранные значения пропускаются.
func BuildProfile(reg Registry, facts []Fact) Profile {
	byKey := map[string]Fact{}
	for _, f := range facts {
		if f.EntityType == EntityHotel {
			byKey[f.Key] = f
		}
	}
	p := Profile{}
	for _, c := range reg.All() {
		f, ok := byKey[c.Key]
		if !ok || c.Level == LevelDistrict {
			continue
		}
		raw, present := f.Value[c.FieldName()]
		switch c.ValueType {
		case ValueNumber:
			if v, ok := toFloat(raw); ok {
				p[c.Key] = ProfileValue{Num: v}
			}
		case ValueBool:
			if b, ok := toBool(raw); ok && present {
				p[c.Key] = ProfileValue{Num: boolNum(b)}
			}
		case ValueEnum:
			s, ok := raw.(string)
			if !ok {
				continue
			}
			if c.Ordered {
				if i := slices.Index(c.Values, s); i >= 0 {
					p[c.Key] = ProfileValue{Num: float64(i)}
				}
			} else {
				p[c.Key] = ProfileValue{Str: s}
			}
		case ValueRange:
			if !present {
				continue
			}
			if v, ok := toFloat(raw); ok {
				p[c.Key] = ProfileValue{Num: v}
			} else if raw == nil {
				p[c.Key] = ProfileValue{None: true}
			}
		}
	}
	return p
}

func boolNum(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// UnknownDistance — расстояние по характеристике, которой нет у одного из отелей: не знаем, значит, наполовину различаются.
const UnknownDistance = 0.5

// DimDistance — расстояние между отелями по одной характеристике, 0..1; различие ниже significant_diff — «одинаково».
func DimDistance(c Characteristic, a, b ProfileValue) float64 {
	switch c.ValueType {
	case ValueNumber:
		d := math.Abs(a.Num - b.Num)
		if c.SignificantDiff > 0 {
			if d < c.SignificantDiff {
				return 0
			}
			return math.Min(1, d/(4*c.SignificantDiff))
		}
		return math.Min(1, d/math.Max(1, math.Max(math.Abs(a.Num), math.Abs(b.Num))))
	case ValueBool:
		return math.Abs(a.Num - b.Num)
	case ValueEnum:
		if c.Ordered {
			if len(c.Values) < 2 {
				return 0
			}
			return math.Abs(a.Num-b.Num) / float64(len(c.Values)-1)
		}
		if a.Str == b.Str {
			return 0
		}
		return 1
	case ValueRange:
		switch {
		case a.None && b.None:
			return 0
		case a.None || b.None:
			return 1
		}
		d := math.Abs(a.Num - b.Num)
		if c.SignificantDiff > 0 && d < c.SignificantDiff {
			return 0
		}
		return math.Min(1, d/profileRangeScaleFloors)
	}
	return 0
}

// Dim — вклад характеристики в расстояние.
type Dim struct {
	Key      string
	Distance float64
	Weight   float64
	Unknown  bool // значения нет у одного из отелей
}

// ProfileDistance — взвешенное среднее расстояние по характеристикам реестра, известным хотя бы у одного отеля.
// Вес по умолчанию 1. Общих характеристик нет совсем — расстояние 1.
func ProfileDistance(reg Registry, a, b Profile, weights map[string]float64) (float64, []Dim) {
	var dims []Dim
	sum, total := 0.0, 0.0
	for _, c := range reg.All() {
		av, aok := a[c.Key]
		bv, bok := b[c.Key]
		if !aok && !bok {
			continue
		}
		w := 1.0
		if x, ok := weights[c.Key]; ok && x > 0 {
			w = x
		}
		d := UnknownDistance
		if aok && bok {
			d = DimDistance(c, av, bv)
		}
		dims = append(dims, Dim{Key: c.Key, Distance: d, Weight: w, Unknown: !(aok && bok)})
		sum += w * d
		total += w
	}
	if total == 0 {
		return 1, nil
	}
	return sum / total, dims
}

// QueryWeights — веса характеристик для «похожего» из запроса: must тяжелее prefer.
func QueryWeights(ws []Desire) map[string]float64 {
	out := map[string]float64{}
	for _, w := range ws {
		x := ProfileWeightPrefer
		if w.Importance == Must {
			x = ProfileWeightMust
		}
		out[w.Key] = math.Max(out[w.Key], x)
	}
	return out
}

// lowConfidence — уверенность ниже MinConfidence с допуском 0,01: свежесть чуть сдвигает уверенность факта из одного
// свидетельства с базой 0,6 (0,5989), и «ровно 0,6» не должно превращаться в «под вопросом».
func lowConfidence(c float64) bool { return c < MinConfidence-0.01 }

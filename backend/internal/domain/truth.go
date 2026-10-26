package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Движок правды: из свидетельств об одной сущности и характеристике получается значение, уверенность, вклад
// каждого свидетельства и признаки расхождения. Чистая функция, без БД; описание и пример — docs/truth.md.

// Виды значения характеристики для движка правды.
const (
	TruthEnum   = "enum"   // enum и bool: сложение логарифмов шансов
	TruthNumber = "number" // число: значение из самого надёжного расчёта, остальные — проверка
	TruthSingle = "single" // значение не сравнивается (карта, структура): берётся сильнейшее свидетельство
)

// Константы поправок надёжности.
const (
	// HotelierAsymmetry — множитель надёжности утверждения отельера в пользу отеля; признание недостатка — 1.0.
	HotelierAsymmetry = 0.75
	// StaleReliabilityFactor — множитель свежести в конце срока годности и после него.
	StaleReliabilityFactor = 0.5
	// DefaultTrust — репутация нового автора.
	DefaultTrust = 0.5
	// MinFactConfidence и MaxFactConfidence ограничивают уверенность факта: абсолютной она не бывает.
	MinFactConfidence = 0.01
	MaxFactConfidence = 0.99
)

// TruthParams — параметры характеристики для движка правды; берутся из реестра (Characteristic.TruthParams).
type TruthParams struct {
	Kind       string
	ValueField string // поле значения в claimed (level, minutes)
	// Order — порядок ступеней упорядоченного enum; пусто — любое различие считается расстоянием 1.
	Order []string
	// ConflictSteps — наименьшее расстояние в ступенях, при котором несогласие считается «против» для конфликта
	// и weak_disagreement; ближе — только снижает уверенность. 0 означает 1.
	ConflictSteps int
	// ConflictMinSide — порог θ: конфликт, если и «за», и «против» весят не меньше (в логарифме шансов).
	ConflictMinSide float64
	// SignificantDiff — для чисел: расхождение больше этого значения — «против».
	SignificantDiff float64
	// FavorableValues — значения, выгодные отелю: к утверждению отельера о них применяется асимметрия.
	FavorableValues []string
	// ConflictEntityTypes — сущности, у которых ищутся конфликты; пусто — у всех. Уровень отеля производный
	// от фасада и своего расхождения не даёт.
	ConflictEntityTypes []string
	// ByEntity — свои правила для сущности другого уровня (категория номеров читает ту же характеристику иначе:
	// другое поле значения, свои значения и порог); нет записи — действуют эти.
	ByEntity map[string]TruthParams
}

// DefaultConflictMinSide — порог θ, если реестр его не задаёт.
const DefaultConflictMinSide = 1.0

// TruthContext — всё, кроме самих свидетельств, что нужно движку: время, репутация авторов, правила.
type TruthContext struct {
	Now   time.Time
	Trust map[Author]float64     // репутация человека-автора; нет в карте — DefaultTrust
	Rules map[string]TruthParams // по ключу характеристики (Registry.Rules); нет правила — TruthSingle
}

func (c TruthContext) params(key, entityType string) TruthParams {
	if p, ok := c.Rules[key]; ok {
		if o, ok := p.ByEntity[entityType]; ok {
			return o
		}
		return p
	}
	return TruthParams{Kind: TruthSingle}
}

// Contribution — вклад свидетельства в итог: надёжность после поправок и подписанный вклад в логарифм шансов
// значения факта (плюс — за итоговое значение, минус — против, 0 — не учтено как зависимое или не сравнимое).
type Contribution struct {
	EvidenceID  int64
	Reliability float64 // r после поправок
	Value       float64
}

// Роли свидетельств в сложении (для описаний).
type role int

const (
	roleFor role = iota
	roleAgainst
	roleIgnored // зависимый источник или значение не разобрано
)

// effectiveReliability считает r = база × репутация × асимметрия × свежесть в границах [MinReliability, MaxReliability].
func effectiveReliability(e Evidence, p TruthParams, c TruthContext) float64 {
	r := e.Reliability
	if e.AuthorType != AuthorTool {
		trust, ok := c.Trust[Author{e.AuthorType, e.AuthorID}]
		if !ok {
			trust = DefaultTrust
		}
		r *= 0.5 + trust
	}
	if e.AuthorType == AuthorHotelier && favorable(e, p) {
		r *= HotelierAsymmetry
	}
	r *= freshness(e, c.Now)
	return math.Min(math.Max(r, MinReliability), MaxReliability)
}

func favorable(e Evidence, p TruthParams) bool {
	v := claimedString(e, p)
	for _, f := range p.FavorableValues {
		if f == v {
			return true
		}
	}
	return false
}

// freshness: 1.0 в момент наблюдения, линейно до StaleReliabilityFactor к концу срока годности и после него.
func freshness(e Evidence, now time.Time) float64 {
	if e.ValidUntil == nil || !e.ValidUntil.After(e.ObservedAt) {
		return 1
	}
	life := e.ValidUntil.Sub(e.ObservedAt).Seconds()
	left := math.Min(math.Max(e.ValidUntil.Sub(now).Seconds(), 0), life)
	return StaleReliabilityFactor + (1-StaleReliabilityFactor)*left/life
}

// logOdds — вклад надёжности r в логарифм шансов: ln(r/(1−r)). Надёжность ниже 0.5 информации не несёт (0).
func logOdds(r float64) float64 {
	return math.Max(math.Log(r/(1-r)), 0)
}

func claimedString(e Evidence, p TruthParams) string {
	if p.ValueField == "" {
		return ""
	}
	v, ok := e.Claimed[p.ValueField]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func claimedNumber(e Evidence, p TruthParams) (float64, bool) {
	switch v := e.Claimed[p.ValueField].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func independenceGroup(e Evidence) string {
	g, _ := e.Details["independence_group"].(string)
	return g
}

// weighed — свидетельство с поправленной надёжностью.
type weighed struct {
	ev    Evidence
	r, l  float64
	role  role
	value string  // enum: значение
	num   float64 // number: значение
}

// dedupGroups помечает свидетельства зависимых источников: из группы остаётся сильнейшее (при равенстве — раннее).
func dedupGroups(ws []weighed) {
	best := map[string]int{}
	for i, w := range ws {
		g := independenceGroup(w.ev)
		if g == "" {
			continue
		}
		if j, ok := best[g]; !ok || w.r > ws[j].r {
			best[g] = i
		}
	}
	for i, w := range ws {
		if g := independenceGroup(w.ev); g != "" && best[g] != i {
			ws[i].role = roleIgnored
		}
	}
}

// stepDistance — расстояние между значениями упорядоченного enum; без порядка любое различие равно 1.
func stepDistance(order []string, a, b string) int {
	if a == b {
		return 0
	}
	ia, ib := indexOf(order, a), indexOf(order, b)
	if ia < 0 || ib < 0 {
		return 1
	}
	if ia > ib {
		return ia - ib
	}
	return ib - ia
}

func indexOf(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}

// verdict — итог сложения свидетельств одной сущности и характеристики.
type verdict struct {
	winner     int // индекс свидетельства, давшего значение
	confidence float64
	forSum     float64 // вес «за»
	againstFar float64 // вес «против», достаточно далёкого для конфликта
	hasFar     bool
}

// AssembleFact собирает факт сущности из её активных свидетельств. Без свидетельств факта нет (false).
// Порядок свидетельств — порядок записи: при равенстве побеждает более раннее.
func AssembleFact(es []Evidence, c TruthContext) (Fact, bool) {
	if len(es) == 0 {
		return Fact{}, false
	}
	p := c.params(es[0].Key, es[0].EntityType)
	ws := make([]weighed, len(es))
	for i, e := range es {
		r := effectiveReliability(e, p, c)
		ws[i] = weighed{ev: e, r: r, l: logOdds(r), role: roleFor}
	}
	dedupGroups(ws)

	var v verdict
	switch p.Kind {
	case TruthEnum:
		v = sumEnum(ws, p)
	case TruthNumber:
		v = sumNumber(ws, p)
	default:
		v = sumSingle(ws)
	}

	w := ws[v.winner]
	f := Fact{
		EntityType: w.ev.EntityType, EntityID: w.ev.EntityID, Key: w.ev.Key, Value: w.ev.Claimed,
		Source: w.ev.Source, Method: w.ev.Method, Confidence: v.confidence,
		ObservedAt: w.ev.ObservedAt, ValidUntil: w.ev.ValidUntil,
		Stale: w.ev.ValidUntil != nil && c.Now.After(*w.ev.ValidUntil),
	}
	for _, x := range ws {
		f.EvidenceIDs = append(f.EvidenceIDs, x.ev.ID)
		contrib := 0.0
		switch x.role {
		case roleFor:
			contrib = x.l
		case roleAgainst:
			contrib = -x.l
		}
		f.Contributions = append(f.Contributions, Contribution{EvidenceID: x.ev.ID, Reliability: x.r, Value: contrib})
	}

	if conflictAllowed(p, f.EntityType) && v.hasFar && v.againstFar > 0 {
		if p.conflictMinSide() > 0 && v.forSum >= p.conflictMinSide() && v.againstFar >= p.conflictMinSide() {
			f.Conflict = &Conflict{Description: describeConflict(ws, p)}
		} else {
			f.WeakDisagreement = true
		}
	}
	return f, true
}

func (p TruthParams) conflictMinSide() float64 {
	if p.ConflictMinSide > 0 {
		return p.ConflictMinSide
	}
	return DefaultConflictMinSide
}

func (p TruthParams) conflictSteps() int {
	if p.ConflictSteps > 0 {
		return p.ConflictSteps
	}
	return 1
}

func conflictAllowed(p TruthParams, entityType string) bool {
	return len(p.ConflictEntityTypes) == 0 || indexOf(p.ConflictEntityTypes, entityType) >= 0
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

func clampConfidence(x float64) float64 {
	return math.Min(math.Max(x, MinFactConfidence), MaxFactConfidence)
}

// sumEnum: S(v) = Σ вклад за v − Σ вклад за остальные; значение с наибольшим S, уверенность σ(S).
// Одно свидетельство с надёжностью r даёт уверенность r.
func sumEnum(ws []weighed, p TruthParams) verdict {
	var cands []string
	for i := range ws {
		ws[i].value = claimedString(ws[i].ev, p)
		if ws[i].role == roleIgnored {
			continue
		}
		if indexOf(cands, ws[i].value) < 0 {
			cands = append(cands, ws[i].value)
		}
	}
	score := func(val string) float64 {
		s := 0.0
		for _, w := range ws {
			switch {
			case w.role == roleIgnored:
			case w.value == val:
				s += w.l
			default:
				s -= w.l
			}
		}
		return s
	}
	win, best := cands[0], score(cands[0])
	for _, c := range cands[1:] {
		if s := score(c); s > best {
			win, best = c, s
		}
	}

	v := verdict{winner: -1, confidence: clampConfidence(sigmoid(best))}
	for i, w := range ws {
		if w.role == roleIgnored {
			continue
		}
		if w.value == win {
			ws[i].role = roleFor
			v.forSum += w.l
			if v.winner < 0 || w.r > ws[v.winner].r {
				v.winner = i
			}
			continue
		}
		ws[i].role = roleAgainst
		if stepDistance(p.Order, w.value, win) >= p.conflictSteps() {
			v.againstFar += w.l
			v.hasFar = true
		}
	}
	return v
}

// sumNumber: значение из самого надёжного расчёта (если расчётов нет — из самого надёжного свидетельства);
// остальные свидетельства проверяют его, расхождение больше SignificantDiff — «против». Числа не голосуют:
// уверенность σ(за − против), значение не меняется.
func sumNumber(ws []weighed, p TruthParams) verdict {
	win := -1
	pick := func(toolsOnly bool) {
		for i, w := range ws {
			if w.role == roleIgnored || (toolsOnly && w.ev.AuthorType != AuthorTool) {
				continue
			}
			if _, ok := claimedNumber(w.ev, p); !ok {
				continue
			}
			if win < 0 || w.r > ws[win].r {
				win = i
			}
		}
	}
	pick(true)
	if win < 0 {
		pick(false)
	}
	if win < 0 {
		return sumSingle(ws)
	}
	base, _ := claimedNumber(ws[win].ev, p)
	v := verdict{winner: win}
	score := 0.0
	for i, w := range ws {
		if w.role == roleIgnored {
			continue
		}
		x, ok := claimedNumber(w.ev, p)
		if !ok {
			ws[i].role = roleIgnored
			continue
		}
		ws[i].num = x
		if math.Abs(x-base) <= p.SignificantDiff {
			ws[i].role = roleFor
			v.forSum += w.l
			score += w.l
		} else {
			ws[i].role = roleAgainst
			v.againstFar += w.l
			v.hasFar = true
			score -= w.l
		}
	}
	v.confidence = clampConfidence(sigmoid(score))
	return v
}

// sumSingle: значение и уверенность сильнейшего свидетельства; остальные не учитываются.
func sumSingle(ws []weighed) verdict {
	win := -1
	for i, w := range ws {
		if w.role == roleIgnored {
			continue
		}
		if win < 0 || w.r > ws[win].r {
			win = i
		}
	}
	for i := range ws {
		if i != win {
			ws[i].role = roleIgnored
		}
	}
	return verdict{winner: win, confidence: clampConfidence(ws[win].r)}
}

// describeConflict перечисляет стороны спора: значение и свидетельства (источник, вклад).
func describeConflict(ws []weighed, p TruthParams) string {
	sides := map[string][]string{}
	var order []string
	for _, w := range ws {
		if w.role == roleIgnored {
			continue
		}
		label := w.value
		if p.Kind == TruthNumber {
			label = fmt.Sprint(w.num)
		}
		if _, ok := sides[label]; !ok {
			order = append(order, label)
		}
		who := w.ev.Source
		if w.ev.AuthorType != AuthorTool {
			who = w.ev.AuthorType
		}
		sides[label] = append(sides[label], fmt.Sprintf("%s %.2f", who, w.l))
	}
	parts := make([]string, 0, len(order))
	for _, label := range order {
		parts = append(parts, fmt.Sprintf("%s=%s: %s", p.ValueField, label, strings.Join(sides[label], ", ")))
	}
	return strings.Join(parts, "; ")
}

// AssembleFacts собирает по факту на каждую пару (сущность, характеристика) среди свидетельств.
// Порядок фактов — по первому появлению пары, порядок свидетельств внутри факта сохраняется.
func AssembleFacts(es []Evidence, c TruthContext) []Fact {
	type pair struct{ entityType, entityID, key string }
	groups := map[pair][]Evidence{}
	var order []pair
	for _, e := range es {
		p := pair{e.EntityType, e.EntityID, e.Key}
		if _, ok := groups[p]; !ok {
			order = append(order, p)
		}
		groups[p] = append(groups[p], e)
	}
	var out []Fact
	for _, p := range order {
		if f, ok := AssembleFact(groups[p], c); ok {
			out = append(out, f)
		}
	}
	return out
}

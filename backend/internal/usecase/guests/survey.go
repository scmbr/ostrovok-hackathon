package guests

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// MaxExpectations — вопросов по ожиданиям брони в анкете (плюс один контрольный).
const MaxExpectations = 2

// controlKeys — характеристики, по которым строится контрольный вопрос: система знает значение уверенно и с запасом.
var controlKeys = []string{"walk_to_metro", "walk_to_beach"}

var controlTargets = map[string]string{"walk_to_metro": "метро", "walk_to_beach": "пляжа"}

// questionTexts — формулировки вопросов по ожиданиям.
var questionTexts = map[string]string{
	domain.SeaViewKey: "Было ли из окна вашего номера видно море?",
	"noise_night":     "Как было ночью в номере: тихо, терпимо или шумно?",
}

// CellSource — значения расчёта в ячейках «фасад × этаж» и преобладающее значение категории (rooms.Cells).
type CellSource interface {
	ValueAt(key string, facadeID int64, floor int) (string, bool)
	Dominant(key string, categoryID int64) (string, bool)
}

var _ CellSource = (*rooms.Cells)(nil)

// Survey — анкета по брони и свидетельства из ответов.
type Survey struct {
	Record   SurveyRecord
	Evidence []domain.Evidence
}

// Env — что нужно для построения анкеты.
type Env struct {
	Registry domain.Registry
	Cells    CellSource
	Facts    []domain.Fact // активные факты отеля: по ним строится контрольный вопрос
	Now      time.Time
}

func nonTarget(values []string, target string) string {
	for _, v := range values {
		if v != target {
			return v
		}
	}
	return target
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// pickControl выбирает контрольный вопрос по фактам отеля: уверенный, свежий факт без расхождений, с запасом
// domain.ControlMargin от порога. Правильный ответ предпочтительно wantYes; нет запаса — другой ответ, если allowOther.
func pickControl(facts []domain.Fact, hotelID int64, wantYes, allowOther bool) (domain.ControlQuestion, bool) {
	id := strconv.FormatInt(hotelID, 10)
	answers := []bool{wantYes}
	if allowOther {
		answers = append(answers, !wantYes)
	}
	for _, yes := range answers {
		for _, key := range controlKeys {
			for _, f := range facts {
				if f.Key != key || f.EntityType != domain.EntityHotel || f.EntityID != id {
					continue
				}
				if f.Confidence < domain.ControlMinConfidence || f.Stale || f.WeakDisagreement || f.Conflict != nil {
					continue
				}
				m, ok := number(f.Value["minutes"])
				if !ok {
					continue
				}
				if q, ok := domain.MakeControl(key, int(m), yes); ok {
					return q, true
				}
			}
		}
	}
	return domain.ControlQuestion{}, false
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// seededRand — детерминированный источник случайных чисел от брони.
func seededRand(b Booking) *rand.Rand { return rand.New(rand.NewSource(b.ID*7727 + b.GuestID*31 + 5)) }

// duration — время заполнения анкеты персоной, секунды: ленивый — за пару секунд.
func duration(persona string, rng *rand.Rand) int {
	switch persona {
	case domain.GuestLazy:
		return 2 + rng.Intn(5)
	case domain.GuestClosedView:
		return 30 + rng.Intn(60)
	default:
		return 35 + rng.Intn(80)
	}
}

// BuildSurvey строит анкету по брони и ответ персоны. Вопросы — по ожиданиям брони (не больше MaxExpectations)
// и один контрольный. Честный гость отвечает значением расчёта в ячейке брони, ленивый — «да» на всё, сообщающий
// о закрытом виде — «вида нет» там, где расчёт вид даёт. Ответ входит в свидетельства, только если расчёт в ячейке брони
// даёт то же значение, что у категории в целом (гость с этажа, где вида нет, категорию с видом не опровергает) и
// анкета не признана небрежной: ответ с весом ленивого гостя (≈0,33) всё равно пробивал порог конфликта категории
// (θ = 0,3) и создавал конфликты на пустом месте, поэтому небрежная анкета снижает репутацию, но свидетельством не становится.
func BuildSurvey(env Env, b Booking) (Survey, error) {
	rng := seededRand(b)
	var s Survey
	s.Record = SurveyRecord{GuestID: b.GuestID, BookingID: b.ID}

	type exp struct {
		c     domain.Characteristic
		calc  string
		value string
	}
	var exps []exp
	for _, key := range b.Expectations {
		c, ok := env.Registry.Get(key)
		if !ok || c.Category == nil || len(exps) >= MaxExpectations {
			continue
		}
		calc, ok := env.Cells.ValueAt(key, b.FacadeID, b.Floor)
		if !ok {
			continue
		}
		rule := c.Category
		e := exp{c: c, calc: calc}
		switch {
		case b.Persona == domain.GuestLazy:
			e.value = rule.Target
		case b.Persona == domain.GuestClosedView && key == domain.SeaViewKey && calc == rule.Target:
			e.value = nonTarget(rule.Values, rule.Target)
		default:
			e.value = calc
		}
		exps = append(exps, e)
	}
	if len(exps) == 0 {
		return s, fmt.Errorf("бронь %d: нет ожиданий с расчётом в ячейке брони", b.ID)
	}

	// Честный гость не должен отвечать одинаково на всё: если по расчёту все ответы по ожиданиям одинаковые (все «да»
	// или все «нет»), правильный ответ контрольного вопроса противоположный. Тогда одинаковые ответы выдают только
	// невнимательного гостя. Нет запаса для противоположного вопроса — контрольный не задаём: иначе честный гость
	// выглядел бы небрежным.
	uniform := true
	for _, e := range exps {
		if (e.calc == e.c.Category.Target) != (exps[0].calc == exps[0].c.Category.Target) {
			uniform = false
		}
	}
	wantYes := b.ID%2 == 0
	if uniform {
		wantYes = exps[0].calc != exps[0].c.Category.Target
	}
	ctrl, hasCtrl := pickControl(env.Facts, b.HotelID, wantYes, !uniform)

	var norm []string
	for _, e := range exps {
		rule := e.c.Category
		q := map[string]any{"kind": "expectation", "key": e.c.Key, "text": questionText(e.c), "options": rule.Values}
		s.Record.Questions = append(s.Record.Questions, q)
		s.Record.Answers = append(s.Record.Answers, map[string]any{"key": e.c.Key, "value": e.value})
		norm = append(norm, yesNo(e.value == rule.Target))
	}
	controlOK := true
	if hasCtrl {
		answer := ctrl.Answer
		if b.Persona == domain.GuestLazy {
			answer = true
		}
		controlOK = answer == ctrl.Answer
		s.Record.Questions = append(s.Record.Questions, map[string]any{
			"kind": "control", "key": ctrl.Key, "threshold_min": ctrl.Threshold, "options": []string{"yes", "no"},
			"text": fmt.Sprintf("От отеля до %s пешком не больше %d минут?", controlTargets[ctrl.Key], ctrl.Threshold),
		})
		s.Record.Answers = append(s.Record.Answers, map[string]any{"key": ctrl.Key, "value": yesNo(answer), "control": true, "correct": yesNo(ctrl.Answer), "minutes": ctrl.Minutes})
		norm = append(norm, yesNo(answer))
	}
	s.Record.ControlOK = controlOK
	s.Record.DurationSec = duration(b.Persona, rng)
	s.Record.Careless = domain.Careless(s.Record.DurationSec, norm, controlOK)
	s.Record.Points = domain.GuruPoints(len(norm), s.Record.Careless)

	for _, e := range exps {
		dom, ok := env.Cells.Dominant(e.c.Key, b.CategoryID)
		inScope := ok && dom == e.calc && len(s.Record.Careless) == 0
		entry := map[string]any{"key": e.c.Key, "facade_id": b.FacadeID, "floor": b.Floor, "calc_value": e.calc, "category_value": dom, "in_scope": inScope}
		switch {
		case len(s.Record.Careless) > 0:
			entry["reason"] = "анкета небрежная: ответы в свидетельства не попадают, репутация гостя снижена"
		case !inScope:
			entry["reason"] = "в ячейке брони расчёт даёт другое значение, чем у категории в целом: категорию ответ не опровергает"
		}
		s.Record.Scope = append(s.Record.Scope, entry)
		if !inScope {
			continue
		}
		until := env.Now.Add(e.c.TTL())
		s.Evidence = append(s.Evidence, domain.Evidence{
			EntityType: domain.EntityRoomCategory, EntityID: strconv.FormatInt(b.CategoryID, 10), Key: e.c.Key,
			Claimed: map[string]any{e.c.Category.ValueField: e.value},
			Source:  domain.SourceGuest, Method: SurveyMethod,
			AuthorType: domain.AuthorGuest, AuthorID: strconv.FormatInt(b.GuestID, 10),
			Reliability: domain.GuestBaseReliability, ObservedAt: env.Now, ValidUntil: &until,
			Details: map[string]any{
				"booking_id": b.ID, "facade_id": b.FacadeID, "floor": b.Floor,
				"calc_value": e.calc, "category_value": dom, "persona": b.Persona,
			},
		})
	}
	return s, nil
}

func questionText(c domain.Characteristic) string {
	if t, ok := questionTexts[c.Key]; ok {
		return t
	}
	return c.Title + ": совпало ли с ожиданием?"
}

// Surveyor проводит анкеты по броням без анкеты и пересчитывает репутацию гостей.
type Surveyor struct {
	Registry   domain.Registry
	Store      Store
	Rooms      Rooms
	Reputation Reputation
	Now        func() time.Time
}

// SurveyResult — итог прогона анкет.
type SurveyResult struct {
	Surveys     int
	ByPersona   map[string]int
	Careless    int
	ControlFail int
	Evidence    int
	OutOfScope  int
	Trust       []TrustChange
}

// TrustChange — изменение репутации гостя.
type TrustChange struct {
	GuestID       int64
	Name, Persona string
	Before, After float64
}

// Run проводит анкеты по всем броням без анкеты (в пределах flt).
func (s *Surveyor) Run(ctx context.Context, flt domain.HotelFilter) (SurveyResult, error) {
	res := SurveyResult{ByPersona: map[string]int{}}
	bookings, err := s.Store.PendingBookings(ctx, flt)
	if err != nil {
		return res, fmt.Errorf("брони без анкеты: %w", err)
	}
	if len(bookings) == 0 {
		return res, fmt.Errorf("нет броней без анкеты (collect mock-stays)")
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	type hotelEnv struct {
		cells CellSource
		facts []domain.Fact
	}
	envs := map[int64]hotelEnv{}
	for _, b := range bookings {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		he, ok := envs[b.HotelID]
		if !ok {
			cells, err := s.Rooms.Cells(ctx, b.HotelID)
			if err != nil {
				return res, fmt.Errorf("отель %d: %w", b.HotelID, err)
			}
			facts, err := s.Store.HotelFacts(ctx, b.HotelID)
			if err != nil {
				return res, fmt.Errorf("отель %d: %w", b.HotelID, err)
			}
			he = hotelEnv{cells, facts}
			envs[b.HotelID] = he
		}
		sv, err := BuildSurvey(Env{Registry: s.Registry, Cells: he.cells, Facts: he.facts, Now: now}, b)
		if err != nil {
			return res, err
		}
		if err := s.Store.SaveSurvey(ctx, sv.Record, sv.Evidence); err != nil {
			return res, fmt.Errorf("бронь %d: %w", b.ID, err)
		}
		up, err := s.Reputation.UpdateGuest(ctx, b.GuestID)
		if err != nil {
			return res, fmt.Errorf("гость %d: %w", b.GuestID, err)
		}
		res.Surveys++
		res.ByPersona[b.Persona]++
		res.Evidence += len(sv.Evidence)
		for _, sc := range sv.Record.Scope {
			if sc["in_scope"] == false {
				res.OutOfScope++
			}
		}
		if len(sv.Record.Careless) > 0 {
			res.Careless++
		}
		if !sv.Record.ControlOK {
			res.ControlFail++
		}
		res.Trust = append(res.Trust, TrustChange{b.GuestID, b.GuestName, b.Persona, up.Before, up.After})
	}
	return res, nil
}

package guests

import (
	"strings"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// fakeCells — расчёт: фасад 1 видит море с 4-го этажа, категория 10 размещена на фасаде 1 (этажи 2–6, вид у 3 из 5 этажей
// по значению «full»), шум на фасаде 1 тихий.
type fakeCells struct{ dominant map[string]string }

func (f fakeCells) ValueAt(key string, facade int64, floor int) (string, bool) {
	switch key {
	case domain.SeaViewKey:
		if floor >= 4 {
			return "full", true
		}
		return "none", true
	case "noise_night":
		return "quiet", true
	}
	return "", false
}

func (f fakeCells) Dominant(key string, _ int64) (string, bool) {
	v, ok := f.dominant[key]
	return v, ok
}

func testEnv(t *testing.T, dominant map[string]string, facts ...domain.Fact) Env {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: domain.SeaViewKey, Level: domain.LevelFacade, Strategies: []domain.Strategy{{Tool: "sea-view"}},
			Category: &domain.CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full"}},
		{Key: "noise_night", Level: domain.LevelFacade, Strategies: []domain.Strategy{{Tool: "noise-night"}},
			Category: &domain.CategoryRule{Values: []string{"quiet", "moderate", "loud"}, ValueField: "level", Target: "quiet"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return Env{Registry: reg, Cells: fakeCells{dominant}, Facts: facts, Now: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)}
}

func metro(minutes int) domain.Fact {
	return domain.Fact{EntityType: domain.EntityHotel, EntityID: "7", Key: "walk_to_metro", Confidence: 0.7, Value: map[string]any{"minutes": float64(minutes)}}
}

func booking(persona string, floor int) Booking {
	return Booking{ID: 1, GuestID: 1, Persona: persona, HotelID: 7, CategoryID: 10, FacadeID: 1, Floor: floor,
		Expectations: []string{domain.SeaViewKey, "noise_night"}}
}

func answerOf(s Survey, key string) string {
	for _, a := range s.Record.Answers {
		if a["key"] == key {
			return a["value"].(string)
		}
	}
	return ""
}

// Честный гость отвечает значением расчёта в своей ячейке; ответ в области, если категория в целом того же значения.
func TestHonestAnswersByCalcInScope(t *testing.T) {
	env := testEnv(t, map[string]string{domain.SeaViewKey: "full", "noise_night": "quiet"}, metro(3))
	s, err := BuildSurvey(env, booking(domain.GuestHonest, 5))
	if err != nil {
		t.Fatal(err)
	}
	if answerOf(s, domain.SeaViewKey) != "full" || answerOf(s, "noise_night") != "quiet" {
		t.Errorf("ответы %v", s.Record.Answers)
	}
	if len(s.Evidence) != 2 || len(s.Record.Careless) != 0 || !s.Record.ControlOK {
		t.Errorf("свидетельств %d, небрежность %v, контроль %v", len(s.Evidence), s.Record.Careless, s.Record.ControlOK)
	}
	for _, e := range s.Evidence {
		if e.AuthorType != domain.AuthorGuest || e.Reliability != domain.GuestBaseReliability || e.Details["floor"] != 5 {
			t.Errorf("свидетельство %+v", e)
		}
	}
}

// Гость со 2-го этажа, где вида нет, категорию с видом не опровергает: его ответ вне области, свидетельства по виду нет.
func TestAnswerOutsideScopeIsNotEvidence(t *testing.T) {
	env := testEnv(t, map[string]string{domain.SeaViewKey: "full", "noise_night": "quiet"}, metro(3))
	s, err := BuildSurvey(env, booking(domain.GuestHonest, 2))
	if err != nil {
		t.Fatal(err)
	}
	if answerOf(s, domain.SeaViewKey) != "none" {
		t.Errorf("честный гость на 2-м этаже должен ответить «вида нет»: %v", s.Record.Answers)
	}
	for _, e := range s.Evidence {
		if e.Key == domain.SeaViewKey {
			t.Errorf("ответ вне области стал свидетельством: %+v", e)
		}
	}
	found := false
	for _, sc := range s.Record.Scope {
		if sc["key"] == domain.SeaViewKey && sc["in_scope"] == false && strings.Contains(sc["reason"].(string), "не опровергает") {
			found = true
		}
	}
	if !found {
		t.Errorf("область %v", s.Record.Scope)
	}
}

// Ленивый отвечает «да» на всё и быстро; репутация у него падает за небрежность, а не за несогласие с расчётом.
func TestLazyIsCareless(t *testing.T) {
	env := testEnv(t, map[string]string{domain.SeaViewKey: "none", "noise_night": "quiet"}, metro(3))
	s, err := BuildSurvey(env, booking(domain.GuestLazy, 2))
	if err != nil {
		t.Fatal(err)
	}
	if answerOf(s, domain.SeaViewKey) != "full" {
		t.Errorf("ленивый должен ответить «да»: %v", s.Record.Answers)
	}
	if len(s.Record.Careless) == 0 || s.Record.Points != 0 {
		t.Errorf("небрежность %v, баллы %d", s.Record.Careless, s.Record.Points)
	}
	if len(s.Evidence) != 0 {
		t.Errorf("небрежная анкета не должна давать свидетельств: %d", len(s.Evidence))
	}
}

// Гость closed_view сообщает о закрытом виде там, где расчёт вид даёт, но внимателен: контрольный пройден, небрежности нет.
func TestClosedViewReportsClosedViewAndStaysReliable(t *testing.T) {
	env := testEnv(t, map[string]string{domain.SeaViewKey: "full"}, metro(12))
	b := booking(domain.GuestClosedView, 5)
	b.Expectations = []string{domain.SeaViewKey}
	s, err := BuildSurvey(env, b)
	if err != nil {
		t.Fatal(err)
	}
	if answerOf(s, domain.SeaViewKey) != "none" {
		t.Errorf("ответы %v", s.Record.Answers)
	}
	if len(s.Record.Careless) != 0 || !s.Record.ControlOK || len(s.Evidence) != 1 || s.Record.Points != 20 {
		t.Errorf("небрежность %v контроль %v свидетельств %d баллы %d", s.Record.Careless, s.Record.ControlOK, len(s.Evidence), s.Record.Points)
	}
}

// Если по расчёту все ответы честного гостя «да», правильный ответ контрольного — «нет»: одинаковые ответы выдают только
// невнимательного. Метро в 3 минутах «нет» не допускает (запас), поэтому контрольный не задаётся вовсе.
func TestControlOppositeToUniformAnswers(t *testing.T) {
	env := testEnv(t, map[string]string{domain.SeaViewKey: "full", "noise_night": "quiet"}, metro(12))
	s, err := BuildSurvey(env, booking(domain.GuestHonest, 5))
	if err != nil {
		t.Fatal(err)
	}
	var control map[string]any
	for _, a := range s.Record.Answers {
		if a["control"] == true {
			control = a
		}
	}
	if control == nil || control["value"] != "no" || len(s.Record.Careless) != 0 {
		t.Errorf("контрольный %v, небрежность %v", control, s.Record.Careless)
	}
	env = testEnv(t, map[string]string{domain.SeaViewKey: "full", "noise_night": "quiet"}, metro(3))
	s, err = BuildSurvey(env, booking(domain.GuestHonest, 5))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Record.Answers) != 2 || len(s.Record.Careless) != 0 {
		t.Errorf("без запаса контрольный не задаётся: %v, небрежность %v", s.Record.Answers, s.Record.Careless)
	}
}

// Контрольный вопрос строится только по уверенному факту без расхождений.
func TestControlSkipsUncertainFacts(t *testing.T) {
	f := metro(3)
	f.Confidence = 0.4
	if _, ok := pickControl([]domain.Fact{f}, 7, true, true); ok {
		t.Error("факт с уверенностью 0,4 не годится для контрольного вопроса")
	}
	f.Confidence, f.Stale = 0.7, true
	if _, ok := pickControl([]domain.Fact{f}, 7, true, true); ok {
		t.Error("устаревший факт не годится")
	}
}

package metrics

import (
	"math"
	"strings"
	"testing"
)

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func input() Input {
	f := func(id int64, key string, conf float64, v map[string]any) HotelFact {
		return HotelFact{HotelID: id, Key: key, Confidence: conf, Value: v}
	}
	return Input{
		DemoHotels: 4,
		Labels: []HotelLabel{
			{HotelID: 1, Group: "coastal", Marks: map[string]Mark{
				TargetSeaView:     {Value: yes(), Confidence: Sure},
				TargetQuietRooms:  {Value: yes(), Confidence: Likely},
				TargetStreetNoisy: {Confidence: Unknown},
			}},
			{HotelID: 2, Group: "urban", Marks: map[string]Mark{
				TargetSeaView:     {Value: no(), Confidence: Likely},
				TargetQuietRooms:  {Value: no(), Confidence: Sure},
				TargetStreetNoisy: {Value: yes(), Confidence: Likely},
			}},
			{HotelID: 3, Group: "urban", Marks: map[string]Mark{
				TargetSeaView: {Value: yes(), Confidence: Sure}, // факта нет
			}},
		},
		Facts: []HotelFact{
			f(1, "sea_view", 0.85, map[string]any{"min_floor": float64(3)}),
			f(1, "noise_night", 0.6, map[string]any{"level": "quiet", "loudest_level": "loud"}),
			f(2, "sea_view", 0.85, map[string]any{"min_floor": nil}),
			f(2, "noise_night", 0.95, map[string]any{"level": "quiet", "loudest_level": "loud"}), // метка «не тихо» — промах
			f(4, "walk_to_beach", 0.7, map[string]any{"minutes": float64(9)}),
		},
		Questions: []QuestionRow{
			{HotelID: 1, Key: "pets_allowed", Status: "answered", Sent: true},
			{HotelID: 2, Key: "pets_allowed", Status: "pending_low_value"},
			{HotelID: 2, Key: "blackout_curtains", Status: "pending"},
			{HotelID: 3, Key: "wish_guarantee", Status: "pending_low_value"},
			{HotelID: 4, Key: "wish_guarantee", Status: "pending"},
		},
		Characteristics: []CharInfo{
			{Key: "sea_view", Title: "Вид на море", Level: "facade"},
			{Key: "walk_to_beach", Title: "Пешком до пляжа", Level: "hotel"},
			{Key: "pets_allowed", Title: "Питомцы", Level: "hotel", AskHotelier: true},
			{Key: "blackout_curtains", Title: "Шторы", Level: "hotel", AskHotelier: true},
			{Key: "wish_guarantee", Title: "Гарантия", Level: "hotel", AskHotelier: true},
		},
	}
}

func TestAccuracyCountsOnlySureAndLikely(t *testing.T) {
	r := Compute(input())
	byTarget := map[string]Accuracy{}
	for _, a := range r.Accuracy {
		byTarget[a.Target] = a
	}
	sv := byTarget[TargetSeaView]
	if sv.Labeled != 3 || sv.Scored != 2 || sv.Correct != 2 || sv.NoFact != 1 || sv.StrictScored != 1 || sv.TP != 1 || sv.TN != 1 {
		t.Errorf("sea_view: %+v", sv)
	}
	q := byTarget[TargetQuietRooms]
	if q.Scored != 2 || q.Correct != 1 || q.FP != 1 || q.StrictScored != 1 || q.StrictCorrect != 0 {
		t.Errorf("quiet_rooms: %+v", q)
	}
	s := byTarget[TargetStreetNoisy]
	if s.Labeled != 2 || s.Unknown != 1 || s.Scored != 1 || s.Correct != 1 {
		t.Errorf("street_noisy: %+v", s)
	}
	if g := sv.ByGroup["coastal"]; g != [2]int{1, 1} {
		t.Errorf("группа береговых: %v", g)
	}
}

func TestCalibrationBuckets(t *testing.T) {
	r := Compute(input())
	got := map[float64][2]int{}
	for _, b := range r.Calibration {
		got[b.From] = [2]int{b.Scored, b.Correct}
	}
	// 0,6 → корзина [0,6; 0,75): quiet_rooms отеля 1 верно; 0,85 → [0,75; 0,9): sea_view ×2 верно + street_noisy верно?
	if got[0.6] != [2]int{1, 1} {
		t.Errorf("[0,60; 0,75): %v", got[0.6])
	}
	if got[0.9] != [2]int{2, 1} {
		t.Errorf("[0,90; 1,00]: %v", got[0.9]) // noise 0,95: quiet_rooms (промах) и street_noisy (верно)
	}
	if got[0.75] != [2]int{2, 2} {
		t.Errorf("[0,75; 0,90): %v", got[0.75])
	}
}

func TestCoverageAndQuestionsSkipWishGuarantee(t *testing.T) {
	r := Compute(input())
	// sea_view — фасадная характеристика, но у отеля есть итоговый факт, поэтому попадает в покрытие; wish_guarantee — нет.
	cov := map[string]CoverageRow{}
	for _, c := range r.Coverage {
		cov[c.Key] = c
	}
	if len(cov) != 4 || cov["wish_guarantee"].Key != "" {
		t.Fatalf("покрытие: %+v", r.Coverage)
	}
	if w := cov["walk_to_beach"]; w.WithFact != 1 || w.Confident != 0 {
		t.Errorf("walk_to_beach: %+v", w)
	}
	if sv := cov["sea_view"]; sv.WithFact != 2 || sv.Confident != 2 {
		t.Errorf("sea_view: %+v", sv)
	}
	if r.QuestionsAll.Candidates != 3 || r.QuestionsAll.Sent != 1 || r.QuestionsAll.LowValue != 1 || r.QuestionsAll.Budget != 1 {
		t.Errorf("вопросы: %+v", r.QuestionsAll)
	}
	if r.CharsAskable != 2 || r.CharsAsked != 1 || r.HotelsAsked != 1 {
		t.Errorf("характеристики %d/%d, отели %d", r.CharsAsked, r.CharsAskable, r.HotelsAsked)
	}
}

func TestAgreement(t *testing.T) {
	a, b, c, d := 40.0, 50.0, 60.0, 62.0
	in := Input{Facades: []FacadeNoise{
		{FacadeID: 1, DemoHotel: true, BCNLevel: "quiet", OSMLevel: "quiet", BCNLn: &a, OSMLn: &a},
		{FacadeID: 2, DemoHotel: true, BCNLevel: "moderate", OSMLevel: "loud", BCNLn: &b, OSMLn: &c},
		{FacadeID: 3, DemoHotel: true, BCNLevel: "quiet", OSMLevel: "loud", BCNLn: &c, OSMLn: &d},
		{FacadeID: 4, DemoHotel: false, BCNLevel: "loud", OSMLevel: "loud"},
		{FacadeID: 5, DemoHotel: true, BCNLevel: "loud"}, // нет OSM
	}}
	ag := Compute(in).Agreement
	if ag.Facades != 3 || ag.Exact != 1 || ag.WithinOne != 2 || ag.Matrix[0][2] != 1 {
		t.Errorf("согласие: %+v", ag)
	}
	if math.IsNaN(ag.Pearson) || ag.Pearson < 0.9 {
		t.Errorf("корреляция: %v", ag.Pearson)
	}
}

func TestRenderMentionsEverySection(t *testing.T) {
	out := Render(Compute(input()))
	for _, want := range []string{"Точность на размеченной выборке", "Калибровка уверенности", "Покрытие демо-отелей", "Вопросы отельерам", "Согласие карты шума"} {
		if !strings.Contains(out, want) {
			t.Errorf("в отчёте нет раздела %q", want)
		}
	}
}

func TestMissesListWrongLabels(t *testing.T) {
	r := Compute(input())
	if len(r.Misses) != 1 || r.Misses[0].HotelID != 2 || r.Misses[0].Target != TargetQuietRooms || r.Misses[0].FactConf != 0.95 {
		t.Errorf("промахи: %+v", r.Misses)
	}
}

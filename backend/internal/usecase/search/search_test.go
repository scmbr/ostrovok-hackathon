package search

import (
	"context"
	"strconv"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

func testRegistry(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: "sea_view", Level: domain.LevelFacade, ValueType: domain.ValueRange, ValueField: "min_floor",
			Category: &domain.CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full", FloorField: "min_floor"}},
		{Key: "walk_to_beach", Level: domain.LevelHotel, ValueType: domain.ValueNumber, ValueField: "minutes", SignificantDiff: 5},
		{Key: "pets_allowed", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// fakeRooms — категории и факты фасадов без БД; отдаёт то, что нужно rooms.Service.
type fakeRooms struct {
	cats    map[int64][]domain.RoomCategory
	facades map[int64][]rooms.FacadeInfo
	facts   map[int64][]domain.Fact
	catFact map[int64][]rooms.CategoryFact
}

func (f *fakeRooms) HotelsWithCategories(context.Context, domain.HotelFilter) ([]int64, error) {
	return nil, nil
}
func (f *fakeRooms) Categories(_ context.Context, id int64) ([]domain.RoomCategory, error) {
	return f.cats[id], nil
}
func (f *fakeRooms) Facades(_ context.Context, id int64) ([]rooms.FacadeInfo, error) {
	return f.facades[id], nil
}
func (f *fakeRooms) FacadeFacts(_ context.Context, id int64, _ []string) ([]domain.Fact, error) {
	return f.facts[id], nil
}
func (f *fakeRooms) CategoryFacts(_ context.Context, id int64) ([]rooms.CategoryFact, error) {
	return f.catFact[id], nil
}
func (f *fakeRooms) SaveCategoryEvidence(context.Context, int64, string, []domain.Author, []domain.Evidence) error {
	return nil
}

type fakeStore struct {
	cands []Candidate
	facts map[int64][]FactView
}

func (s fakeStore) Candidates(context.Context, int64) ([]Candidate, error) { return s.cands, nil }
func (s fakeStore) Candidate(_ context.Context, id int64) (Candidate, error) {
	for _, c := range s.cands {
		if c.ID == id {
			return c, nil
		}
	}
	return Candidate{}, ErrNotFound
}
func (s fakeStore) HotelFacts(_ context.Context, id int64) ([]FactView, error) {
	return s.facts[id], nil
}

type recorder struct{ calls [][]string }

func (r *recorder) Record(_ context.Context, _ int64, _ string, keys []string) error {
	r.calls = append(r.calls, keys)
	return nil
}

func facadeFact(id int64, minFloor any) domain.Fact {
	return domain.Fact{ID: id, EntityType: domain.EntityFacade, EntityID: strconv.FormatInt(id, 10), Key: "sea_view",
		Value: map[string]any{"min_floor": minFloor}, Confidence: 0.85}
}

func catFact(cat int64, view string, conf float64, conflict string) rooms.CategoryFact {
	f := domain.Fact{ID: 100 + cat, EntityType: domain.EntityRoomCategory, EntityID: strconv.FormatInt(cat, 10), Key: "sea_view",
		Value: map[string]any{"view": view}, Confidence: conf}
	if conflict != "" {
		f.Conflict = &domain.Conflict{Description: conflict}
	}
	return rooms.CategoryFact{CategoryID: cat, Fact: f}
}

func hotelFact(key string, value map[string]any, conf float64) FactView {
	return FactView{Fact: domain.Fact{ID: 1, EntityType: domain.EntityHotel, Key: key, Value: value, Confidence: conf}}
}

// Три отеля: честный «Sea View» с видом, приукрашивающий «Sea View» без вида и отель без вида вообще.
func newTestService(t *testing.T) (*Service, *recorder) {
	t.Helper()
	pl := func(facade int64, from, to, n int) []domain.StoredPlacement {
		return []domain.StoredPlacement{{Placement: domain.Placement{FacadeID: facade, FloorFrom: from, FloorTo: to, Rooms: n}}}
	}
	fr := &fakeRooms{
		cats: map[int64][]domain.RoomCategory{
			1: {{ID: 11, HotelID: 1, Name: "Standard", Rooms: 10, Placements: pl(1, 1, 10, 10)},
				{ID: 12, HotelID: 1, Name: "Sea View", Rooms: 10, Placements: pl(1, 8, 10, 10)}},
			2: {{ID: 21, HotelID: 2, Name: "Sea View", Rooms: 10, Placements: pl(2, 1, 5, 10)}},
			3: {{ID: 31, HotelID: 3, Name: "Standard", Rooms: 10, Placements: pl(3, 1, 5, 10)}},
		},
		facades: map[int64][]rooms.FacadeInfo{1: {{ID: 1, AzimuthDeg: 90}}, 2: {{ID: 2, AzimuthDeg: 180}}, 3: {{ID: 3, AzimuthDeg: 270}}},
		facts: map[int64][]domain.Fact{
			1: {facadeFact(1, 8.0)},
			2: {facadeFact(2, nil)},
			3: {facadeFact(3, nil)},
		},
		catFact: map[int64][]rooms.CategoryFact{
			1: {catFact(11, "none", 0.85, ""), catFact(12, "full", 0.9, "")},
			2: {catFact(21, "none", 0.79, "название обещает вид, расчёт против")},
			3: {catFact(31, "none", 0.85, "")},
		},
	}
	reg := testRegistry(t)
	store := fakeStore{
		cands: []Candidate{
			{ID: 1, Name: "Honest", Card: domain.Card{Stars: 4, PriceEUR: 100}},
			{ID: 2, Name: "Embellisher", Card: domain.Card{Stars: 3, PriceEUR: 80}},
			{ID: 3, Name: "Plain", Card: domain.Card{Stars: 3, PriceEUR: 70}},
		},
		facts: map[int64][]FactView{
			1: {hotelFact("walk_to_beach", map[string]any{"minutes": 6.0}, 0.7)},
			2: {hotelFact("walk_to_beach", map[string]any{"minutes": 20.0}, 0.7)},
			3: {hotelFact("walk_to_beach", map[string]any{"minutes": 9.0}, 0.7)},
		},
	}
	rec := &recorder{}
	return &Service{Registry: reg, Store: store, Rooms: &rooms.Service{Registry: reg, Store: fr}, Demand: rec, RegionID: 1}, rec
}

func byID(res Result, id int64) (domain.HotelResult, int) {
	for i, h := range res.Hotels {
		if h.HotelID == id {
			return h, i
		}
	}
	return domain.HotelResult{}, -1
}

func TestSearchSeaViewMustGuaranteeAndEmbellisher(t *testing.T) {
	s, _ := newTestService(t)
	res, err := s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "sea_view", Importance: domain.Must}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hotels) != 3 || res.Hotels[0].HotelID != 1 {
		t.Fatalf("выдача: %+v", res.Hotels)
	}
	honest := res.Hotels[0]
	if honest.Best.Name != "Sea View" || !honest.Best.Guaranteed || honest.Best.PriceEUR != 130 {
		t.Errorf("честный отель: лучший вариант %+v", honest.Best)
	}
	if honest.Alt == nil || honest.Alt.Name != "Standard" {
		t.Fatalf("альтернатива в Standard (дешевле): %+v", honest.Alt)
	}
	// Standard: вид с 8-го этажа, 3 номера из 10; шанс 30 % и пожелание «восточная сторона, этаж 8+».
	e := honest.Alt.Evals[0]
	if e.Status != domain.StatusChance || e.PBase != 0.3 || e.Wish == nil || e.Wish.MinFloor != 8 || e.Wish.AzimuthDeg != 90 {
		t.Errorf("шанс в Standard: %+v", e)
	}
	// Приукрашивающий отель: «Sea View» не даёт гарантии, конфликт виден, must нарушен, отель в конце.
	emb, idx := byID(res, 2)
	ee := emb.Best.Evals[0]
	if emb.Best.Guaranteed || ee.Guarantee || ee.Status != domain.StatusMismatch || ee.Conflict == "" || len(emb.Best.Violations) != 1 || idx == 0 {
		t.Errorf("приукрашивающий: место %d, %+v", idx, ee)
	}
	if len(ee.Sources) != 0 {
		t.Errorf("источники из фейка не заданы: %v", ee.Sources)
	}
}

func TestSearchFiltersAndUnknown(t *testing.T) {
	s, _ := newTestService(t)
	// Бюджет: Sea View у честного отеля стоит 130, вариант Standard — 100.
	res, err := s.Search(context.Background(), domain.Query{
		Wishes:  []domain.Desire{{Key: "sea_view", Importance: domain.Prefer}},
		Filters: domain.Filters{PriceMax: 100, StarsMin: 3}})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := byID(res, 1)
	if h.Best.Name != "Standard" || h.Alt != nil {
		t.Errorf("при бюджете 100 остаётся Standard: %+v", h.Best)
	}
	// Питомцы: фактов нет ни у кого — «спросим отель», отели не исключаются.
	res, err = s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "pets_allowed", Importance: domain.Must}}})
	if err != nil || len(res.Hotels) != 3 {
		t.Fatalf("неизвестное не исключает: %v %d", err, len(res.Hotels))
	}
	for _, h := range res.Hotels {
		if len(h.Best.Violations) != 0 || len(h.Best.Unknown) != 1 || h.Best.Evals[0].Status != domain.StatusUnknown {
			t.Errorf("отель %d: %+v", h.HotelID, h.Best)
		}
	}
}

func TestSearchWalkNear(t *testing.T) {
	s, _ := newTestService(t)
	res, err := s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "walk_to_beach", Value: 10, Importance: domain.Must}}})
	if err != nil {
		t.Fatal(err)
	}
	// 6 мин — совпало; 9 мин при significant_diff 5 — на границе, не нарушение; 20 мин — нарушение.
	want := map[int64]domain.Status{1: domain.StatusNear, 3: domain.StatusNear, 2: domain.StatusMismatch}
	for id, st := range want {
		h, _ := byID(res, id)
		if got := h.Best.Evals[0].Status; got != st {
			t.Errorf("отель %d: %s, ждали %s", id, got, st)
		}
	}
	if h, _ := byID(res, 3); len(h.Best.Violations) != 0 {
		t.Error("«почти» не нарушение")
	}
}

func TestSearchValidationAndDemand(t *testing.T) {
	s, rec := newTestService(t)
	ctx := context.Background()
	for _, q := range []domain.Query{
		{},
		{Wishes: []domain.Desire{{Key: "ghost"}}},
		{Wishes: []domain.Desire{{Key: "sea_view"}, {Key: "sea_view"}}},
	} {
		var ve *ValidationError
		if _, err := s.Search(ctx, q); err == nil || !asValidation(err, &ve) {
			t.Errorf("ждали ошибку проверки для %+v, получили %v", q, err)
		}
	}
	if len(rec.calls) != 0 {
		t.Error("неверный запрос спрос не пишет")
	}
	if _, err := s.Search(ctx, domain.Query{SessionID: "s1", Wishes: []domain.Desire{{Key: "sea_view"}, {Key: "walk_to_beach", Value: 10}}}); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 || len(rec.calls[0]) != 2 {
		t.Errorf("спрос: %v", rec.calls)
	}
	s.Demand = nil
	if _, err := s.Search(ctx, domain.Query{Wishes: []domain.Desire{{Key: "sea_view"}}}); err != nil {
		t.Fatal(err)
	}
}

func asValidation(err error, target **ValidationError) bool {
	ve, ok := err.(*ValidationError)
	*target = ve
	return ok
}

func TestSimilarUsesFactsNotStarsOrPrice(t *testing.T) {
	s, _ := newTestService(t)
	// Профили: 1 — 6 мин, 3 — 9 мин (в пределах significant_diff = 5 → «одинаково»), 2 — 20 мин.
	got, err := s.Similar(context.Background(), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 3 || got[0].Distance != 0 || got[1].ID != 2 || got[1].Distance <= 0 {
		t.Fatalf("похожие: %+v", got)
	}
}

func TestHotelCard(t *testing.T) {
	s, _ := newTestService(t)
	h, err := s.Hotel(context.Background(), 2)
	if err != nil || h.Rooms == nil || len(h.Rooms.Categories) != 1 || len(h.Facts) != 1 {
		t.Fatalf("карточка: %v %+v", err, h)
	}
	if _, err := s.Hotel(context.Background(), 99); err != ErrNotFound {
		t.Errorf("ждали ErrNotFound, получили %v", err)
	}
}

package rooms

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func testRegistry(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{{
		Key: domain.SeaViewKey, Title: "Вид на море", Level: domain.LevelFacade, ValueType: domain.ValueRange,
		ValueField: "min_floor", TTLDays: 365,
		Category: &domain.CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full", FloorField: "min_floor"},
	}, {
		Key: domain.NoiseNightKey, Title: "Тишина", Level: domain.LevelFacade, ValueType: domain.ValueEnum,
		Values: []string{domain.NoiseQuiet, domain.NoiseModerate, domain.NoiseLoud}, ValueField: "level", TTLDays: 365,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

type fakeStore struct {
	cats    []domain.RoomCategory
	facts   []domain.Fact
	saved   map[int64][]domain.Evidence // по категории
	authors map[int64][]domain.Author
}

func (f *fakeStore) HotelsWithCategories(context.Context, domain.HotelFilter) ([]int64, error) {
	return []int64{1}, nil
}
func (f *fakeStore) Categories(context.Context, int64) ([]domain.RoomCategory, error) {
	return f.cats, nil
}
func (f *fakeStore) Facades(context.Context, int64) ([]FacadeInfo, error) { return nil, nil }
func (f *fakeStore) FacadeFacts(_ context.Context, _ int64, keys []string) ([]domain.Fact, error) {
	var out []domain.Fact
	for _, x := range f.facts {
		for _, k := range keys {
			if x.Key == k {
				out = append(out, x)
			}
		}
	}
	return out, nil
}
func (f *fakeStore) CategoryFacts(context.Context, int64) ([]CategoryFact, error) { return nil, nil }
func (f *fakeStore) SaveCategoryEvidence(_ context.Context, id int64, _ string, a []domain.Author, evs []domain.Evidence) error {
	if f.saved == nil {
		f.saved, f.authors = map[int64][]domain.Evidence{}, map[int64][]domain.Author{}
	}
	f.saved[id], f.authors[id] = evs, a
	return nil
}

func seaFact(id int64, facade string, minFloor any, conf float64) domain.Fact {
	return domain.Fact{ID: id, EntityType: domain.EntityFacade, EntityID: facade, Key: domain.SeaViewKey,
		Value: map[string]any{"min_floor": minFloor, "levels": float64(8)}, Confidence: conf}
}

func place(facade int64, from, to, rooms int) domain.StoredPlacement {
	return domain.StoredPlacement{Placement: domain.Placement{FacadeID: facade, FloorFrom: from, FloorTo: to, Rooms: rooms}}
}

func TestProjectorWritesToolEvidence(t *testing.T) {
	store := &fakeStore{
		facts: []domain.Fact{seaFact(10, "1", float64(4), 0.85), seaFact(11, "2", nil, 0.85)},
		cats: []domain.RoomCategory{
			{ID: 100, Name: "Sea View", Rooms: 6, Placements: []domain.StoredPlacement{place(1, 4, 7, 6)}},
			{ID: 101, Name: "Standard", Rooms: 20, Placements: []domain.StoredPlacement{place(1, 2, 6, 5), place(2, 1, 4, 15)}},
			{ID: 102, Name: "Fantasy", Rooms: 4, Placements: []domain.StoredPlacement{place(9, 1, 3, 4)}}, // фасад без факта
		},
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	res, err := (&Projector{Registry: testRegistry(t), Store: store, Now: func() time.Time { return now }}).Run(context.Background(), domain.HotelFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hotels != 1 || res.Categories != 3 || res.Evidence != 2 {
		t.Errorf("итог %+v, ждали отель 1, категорий 3, свидетельств 2 (у «Fantasy» нет фактов фасадов)", res)
	}

	sv := store.saved[100]
	if len(sv) != 1 {
		t.Fatalf("свидетельств у «Sea View»: %d", len(sv))
	}
	e := sv[0]
	if e.Claimed["view"] != "full" || e.Claimed["guarantee"] != true || e.Claimed["probability"] != 1.0 {
		t.Errorf("«Sea View»: %v", e.Claimed)
	}
	if e.EntityType != domain.EntityRoomCategory || e.EntityID != "100" || e.Source != domain.SourceComputed ||
		e.AuthorType != domain.AuthorTool || e.Method != domain.RoomProjectionMethod {
		t.Errorf("автор и сущность: %+v", e)
	}
	if math.Abs(e.Reliability-0.85) > 1e-9 {
		t.Errorf("надёжность %.3f, ждали уверенность фасада 0,85 при доле 100%%", e.Reliability)
	}
	if e.ValidUntil == nil || !e.ValidUntil.Equal(now.Add(365*24*time.Hour)) {
		t.Errorf("срок годности %v", e.ValidUntil)
	}

	// «Standard»: фасад 1 этажи 2–6, 5 номеров, вид с 4-го: 3 этажа из 5 → 3 номера; фасад 2 вида нет. 3/20 = 15 %.
	st := store.saved[101][0]
	if st.Claimed["view"] != "none" || st.Claimed["probability"] != 0.15 || st.Claimed["guarantee"] != false {
		t.Errorf("«Standard»: %v", st.Claimed)
	}
	if want := 0.85 * 0.85; math.Abs(st.Reliability-want) > 1e-9 {
		t.Errorf("надёжность смешанной категории %.4f, ждали уверенность × доля преобладающего значения = %.4f", st.Reliability, want)
	}
	wish, _ := st.Claimed["wish"].(map[string]any)
	if wish["facade_id"] != int64(1) || wish["min_floor"] != 4 {
		t.Errorf("пожелание: %v", wish)
	}
	if len(store.saved[102]) != 0 {
		t.Error("без фактов фасадов свидетельства быть не должно: прежнее только снимается")
	}
	for id, a := range store.authors {
		if len(a) != 1 || a[0] != domain.ToolAuthor(domain.RoomProjectionMethod) {
			t.Errorf("категория %d: снимаются свидетельства авторов %v, а должны только расчётные", id, a)
		}
	}
}

func TestServiceHotel(t *testing.T) {
	store := &fakeStore{
		facts: []domain.Fact{seaFact(10, "1", float64(4), 0.85)},
		cats:  []domain.RoomCategory{{ID: 100, Name: "Sea View", Rooms: 6, Placements: []domain.StoredPlacement{place(1, 5, 8, 6)}}},
	}
	h, err := (&Service{Registry: testRegistry(t), Store: store}).Hotel(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Categories) != 1 || len(h.Categories[0].Characteristics) != 1 {
		t.Fatalf("характеристик в категории: %+v (в реестре одна с category:)", h.Categories)
	}
	c := h.Categories[0].Characteristics[0]
	if c.Key != domain.SeaViewKey || !c.Assessment.Guarantee || c.Assessment.Probability != 1 || c.Assessment.Wish != nil {
		t.Errorf("оценка: %+v", c)
	}
	if _, err := (&Service{Registry: testRegistry(t), Store: &fakeStore{}}).Hotel(context.Background(), 1); err != ErrNotFound {
		t.Errorf("отель без категорий: %v", err)
	}
}

// Факт фасада изменился (инструмент записал новый вид) — Refresh пересобирает свидетельство категории,
// вероятность следует за фасадом без отдельного запуска проекции.
func TestRefreshFollowsFacadeFacts(t *testing.T) {
	store := &fakeStore{
		facts: []domain.Fact{seaFact(10, "1", float64(4), 0.85)},
		cats:  []domain.RoomCategory{{ID: 101, Name: "Standard", Rooms: 10, Placements: []domain.StoredPlacement{place(1, 1, 5, 10)}}},
	}
	pr := &Projector{Registry: testRegistry(t), Store: store}
	ctx := context.Background()
	probability := func() any { return store.saved[101][0].Claimed["probability"] }

	if err := pr.Refresh(ctx, 1, domain.SeaViewKey); err != nil {
		t.Fatal(err)
	}
	if got := probability(); got != 0.4 { // вид с 4-го этажа: этажи 4–5 из 5
		t.Fatalf("вероятность до изменения фасада %v, ждали 0,4", got)
	}
	store.facts = []domain.Fact{seaFact(11, "1", float64(2), 0.85)} // фасад стал видеть море с 2-го этажа
	if err := pr.Refresh(ctx, 1, domain.SeaViewKey); err != nil {
		t.Fatal(err)
	}
	if got := probability(); got != 0.8 {
		t.Errorf("вероятность после изменения фасада %v, ждали 0,8", got)
	}

	// Характеристика без правила category и отель без категорий — не ошибка и не запись.
	store.saved = nil
	if err := pr.Refresh(ctx, 1, domain.NoiseNightKey); err != nil || store.saved != nil {
		t.Errorf("noise_night без category: ошибка %v, записано %v", err, store.saved)
	}
	empty := &Projector{Registry: testRegistry(t), Store: &fakeStore{}}
	if err := empty.Refresh(ctx, 1, domain.SeaViewKey); err != nil {
		t.Errorf("отель без категорий: %v", err)
	}
}

func TestCategoryImpact(t *testing.T) {
	store := &fakeStore{
		facts: []domain.Fact{seaFact(10, "1", float64(4), 0.45), seaFact(11, "2", float64(4), 0.9)},
		cats: []domain.RoomCategory{
			{ID: 100, Rooms: 6, Placements: []domain.StoredPlacement{place(1, 5, 8, 6)}}, // на неуверенном фасаде
			{ID: 101, Rooms: 6, Placements: []domain.StoredPlacement{place(2, 5, 8, 6)}}, // на уверенном
		},
	}
	changed, total, err := (&Service{Registry: testRegistry(t), Store: store}).CategoryImpact(context.Background(), 1, domain.SeaViewKey)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || total != 2 {
		t.Errorf("ответ меняет вероятность в 1 из 2 категорий: %d из %d", changed, total)
	}
	// Характеристика без правила category: категориям нечего менять.
	if c, n, _ := (&Service{Registry: testRegistry(t), Store: store}).CategoryImpact(context.Background(), 1, domain.NoiseNightKey); c != 0 || n != 0 {
		t.Errorf("без category: %d из %d", c, n)
	}
}

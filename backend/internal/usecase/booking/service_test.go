package booking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

func testRegistry(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{
		{Key: "sea_view", Title: "Вид на море", Level: domain.LevelFacade, ValueType: domain.ValueRange, ValueField: "min_floor",
			Category: &domain.CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full", FloorField: "min_floor"}},
		{Key: "pets_allowed", Title: "Можно с питомцем", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", AskHotelier: "?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// fakeSearch отдаёт заранее заданные варианты: у каждого отеля Standard с шансом ~25 % и Vista mar с гарантией.
type fakeSearch struct{}

func seaEval(status domain.Status, p float64, guarantee bool, wish *domain.VariantWish) domain.WishEval {
	return domain.WishEval{Key: "sea_view", Importance: domain.Prefer, Status: status, P: p, PBase: p, Score: domain.WishScore(p, 0.85),
		Confidence: 0.85, Guarantee: guarantee, Wish: wish}
}

func petsUnknown() domain.WishEval {
	return domain.WishEval{Key: "pets_allowed", Importance: domain.Prefer, Status: domain.StatusUnknown, Score: domain.UnknownScore}
}

func (fakeSearch) EvalHotel(_ context.Context, id int64, q domain.Query) (search.HotelVariants, domain.Query, error) {
	if len(q.Wishes) == 0 {
		return search.HotelVariants{}, q, &search.ValidationError{Msg: "в сессии нет желаний: сравнивать не по чему"}
	}
	if id > 2 {
		return search.HotelVariants{}, q, search.ErrNotFound
	}
	wish := &domain.VariantWish{FacadeID: 11, AzimuthDeg: 90, MinFloor: 4, Probability: 0.7 * 0.8}
	std := domain.NewVariant(10, "Standard", 20, 100, []domain.WishEval{seaEval(domain.StatusChance, 0.25, false, wish), petsUnknown()}, map[string]bool{"sea_view": true})
	vista := domain.NewVariant(20, "Vista mar", 10, 130, []domain.WishEval{seaEval(domain.StatusMatch, 1, true, nil), petsUnknown()}, map[string]bool{"sea_view": true})
	best := vista
	if id == 2 {
		best = std
	}
	return search.HotelVariants{Result: domain.HotelResult{HotelID: id, Name: map[int64]string{1: "Hotel A", 2: "Hotel B"}[id], Best: best},
		Variants: []domain.Variant{std, vista}}, q, nil
}

func (fakeSearch) Hotels(context.Context) ([]search.Candidate, error) {
	return []search.Candidate{{ID: 1, Name: "Hotel A"}, {ID: 2, Name: "Hotel B"}}, nil
}

type memSessions map[string][]byte

func (m memSessions) LoadState(_ context.Context, id string) ([]byte, bool, error) {
	b, ok := m[id]
	return b, ok, nil
}
func (m memSessions) SaveState(_ context.Context, id string, b []byte) error { m[id] = b; return nil }

type fakeHotelier struct {
	confirm  []string
	answered bool
	yes      bool
	asked    []string
	q        *hotelier.Question
}

func (f *fakeHotelier) AskAbout(_ context.Context, _ int64, key string) (hotelier.Ask, error) {
	f.asked = append(f.asked, key)
	if f.q != nil {
		return hotelier.Ask{Question: f.q, Reused: true, Sent: true}, nil
	}
	f.q = &hotelier.Question{ID: 5, Key: key, Status: hotelier.StatusOpen}
	return hotelier.Ask{Question: f.q, Created: true, Sent: true}, nil
}

func (f *fakeHotelier) ConfirmWish(_ context.Context, _ int64, category, wish string) (hotelier.Ask, error) {
	f.confirm = append(f.confirm, category+"|"+wish)
	if f.answered {
		return hotelier.Ask{Answered: true, Guarantees: f.yes}, nil
	}
	reused := f.q != nil
	if !reused {
		f.q = &hotelier.Question{ID: 9, Key: domain.WishGuaranteeKey, Status: hotelier.StatusOpen}
	}
	return hotelier.Ask{Question: f.q, Created: !reused, Reused: reused, Sent: true}, nil
}

func (f *fakeHotelier) WishAnswer(context.Context, int64) (bool, bool, error) {
	return f.answered, f.yes, nil
}

type memBookings struct{ rows []Record }

func (m *memBookings) CreateBooking(_ context.Context, r Record) (Record, error) {
	r.ID = int64(len(m.rows) + 1)
	m.rows = append(m.rows, r)
	return r, nil
}
func (m *memBookings) Booking(_ context.Context, id int64) (Record, error) {
	if id < 1 || int(id) > len(m.rows) {
		return Record{}, ErrNotFound
	}
	return m.rows[id-1], nil
}

type memTrace []agent.TraceStep

func (m *memTrace) AppendTrace(_ context.Context, s agent.TraceStep) error {
	*m = append(*m, s)
	return nil
}

type memDemand map[string]int

func (m memDemand) Record(_ context.Context, _ int64, session string, keys []string) error {
	for _, k := range keys {
		m[session+"/"+k]++
	}
	return nil
}

type rig struct {
	s        *Service
	sessions memSessions
	hotelier *fakeHotelier
	bookings *memBookings
	trace    *memTrace
	demand   memDemand
}

const sessionJSON = `{"wishes":[{"key":"sea_view","op":"eq","value":"full","importance":"prefer"},{"key":"pets_allowed","importance":"prefer"}],"asked":1,"last":[{"id":1,"name":"Hotel A"}]}`

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{sessions: memSessions{"s1": []byte(sessionJSON)}, hotelier: &fakeHotelier{}, bookings: &memBookings{}, trace: &memTrace{}, demand: memDemand{}}
	r.s = &Service{Registry: testRegistry(t), Search: fakeSearch{}, Sessions: r.sessions, Hotelier: r.hotelier, Demand: r.demand,
		Bookings: r.bookings, Trace: r.trace, RegionID: 1}
	return r
}

// Шортлист пишется в то же состояние сессии, не теряя остальные поля диалога.
func TestShortlistKeepsDialogState(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.s.AddToShortlist(ctx, "s1", 1); err != nil {
		t.Fatal(err)
	}
	list, err := r.s.AddToShortlist(ctx, "s1", 2)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	if again, _ := r.s.AddToShortlist(ctx, "s1", 2); len(again) != 2 {
		t.Error("повтор не должен дублировать")
	}
	var st map[string]json.RawMessage
	if err := json.Unmarshal(r.sessions["s1"], &st); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"wishes", "asked", "last", "shortlist"} {
		if _, ok := st[k]; !ok {
			t.Errorf("в состоянии пропало поле %q", k)
		}
	}
	list, _ = r.s.RemoveFromShortlist(ctx, "s1", 1)
	if len(list) != 1 || list[0].ID != 2 {
		t.Errorf("после удаления: %v", list)
	}
	if _, err := r.s.AddToShortlist(ctx, "s1", 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий отель: %v", err)
	}
}

func TestCompareValidationAndTrace(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for name, req := range map[string]CompareRequest{
		"один отель":   {SessionID: "s1", HotelIDs: []int64{1}},
		"дубль":        {SessionID: "s1", HotelIDs: []int64{1, 1}},
		"ничего":       {SessionID: "s1"},
		"нет желаний":  {SessionID: "empty", HotelIDs: []int64{1, 2}},
		"категория 99": {SessionID: "s1", HotelID: 1, CategoryIDs: []int64{10, 99}},
	} {
		if _, err := r.s.Compare(ctx, req); err == nil || (!errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound)) {
			t.Errorf("%s: ошибка %v", name, err)
		}
	}
	if _, err := r.s.Compare(ctx, CompareRequest{SessionID: "s1", HotelIDs: []int64{1, 99}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("неизвестный отель: %v", err)
	}
	c, err := r.s.Compare(ctx, CompareRequest{SessionID: "s1", HotelIDs: []int64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Comparison.Winner != 0 || !strings.Contains(c.Comparison.Verdict, "Hotel A") {
		t.Errorf("вывод: %d %s", c.Comparison.Winner, c.Comparison.Verdict)
	}
	if n := len(*r.trace); n != 1 || (*r.trace)[0].Agent != AgentName || (*r.trace)[0].SessionID != "s1" || (*r.trace)[0].Tool != "compare" {
		t.Errorf("журнал: %+v", *r.trace)
	}
}

// Категории одного отеля: Standard с пожеланием против Vista mar с гарантией.
func TestCompareCategoriesOfOneHotel(t *testing.T) {
	r := newRig(t)
	c, err := r.s.Compare(context.Background(), CompareRequest{SessionID: "s1", HotelID: 1, CategoryIDs: []int64{10, 20}})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Categories || c.Comparison.Winner != 1 {
		t.Fatalf("%+v", c.Comparison)
	}
	for _, want := range []string{"+30 €", "гарантирован", "шанс ~25 %"} {
		if !strings.Contains(c.Comparison.Tradeoff, want) {
			t.Errorf("цена компромисса без %q: %s", want, c.Comparison.Tradeoff)
		}
	}
	if len(c.Comparison.Uncertain) != 2 {
		t.Errorf("неуверенное (питомцы у обеих категорий): %+v", c.Comparison.Uncertain)
	}
}

func TestContractAndAsk(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	c, err := r.s.Contract(ctx, ContractRequest{SessionID: "s1", HotelID: 1, CategoryID: 10, WithWish: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Offer == nil || c.Lines[0].Kind != domain.ContractWishPending || !c.Lines[1].CanAsk {
		t.Fatalf("%+v", c.Lines)
	}
	// у Vista mar пожелания нет: гарантия
	if _, err := r.s.Contract(ctx, ContractRequest{SessionID: "s1", HotelID: 1, CategoryID: 20, WithWish: true}); !errors.Is(err, ErrInvalid) {
		t.Errorf("пожелание при гарантии: %v", err)
	}
	// «спросить отель» дважды: вопрос один, спрос учтён за сессию
	a1, err := r.s.AskHotel(ctx, AskRequest{SessionID: "s1", HotelID: 1, Key: "pets_allowed"})
	if err != nil || !a1.Ask.Created || !strings.Contains(a1.Message, "Спросили отель") {
		t.Fatalf("%+v %v", a1, err)
	}
	a2, _ := r.s.AskHotel(ctx, AskRequest{SessionID: "s1", HotelID: 1, Key: "pets_allowed"})
	if a2.Ask.Created || !a2.Ask.Reused || a2.Ask.Question.ID != a1.Ask.Question.ID {
		t.Errorf("повтор: %+v", a2.Ask)
	}
	if _, err := r.s.AskHotel(ctx, AskRequest{SessionID: "s1", HotelID: 1, Key: "ghost"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("чужая характеристика: %v", err)
	}
}

func wishReq(cat int64) BookRequest {
	return BookRequest{SessionID: "s1", HotelID: 1, CategoryID: cat, FacadeID: 11, MinFloor: 4}
}

func TestBookWithWishStatuses(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	b, err := r.s.Book(ctx, wishReq(10))
	if err != nil {
		t.Fatal(err)
	}
	if b.WishStatus != WishWaiting || b.Question == nil || len(r.hotelier.confirm) != 1 || r.hotelier.confirm[0] != "Standard|восточная сторона, этаж 4+" {
		t.Fatalf("%+v %v", b, r.hotelier.confirm)
	}
	rec, _ := r.bookings.Booking(ctx, b.ID)
	if rec.WishQuestionID != 9 || rec.Wish["text"] != "восточная сторона, этаж 4+" || len(rec.Expectations) != 2 {
		t.Errorf("запись брони: %+v", rec)
	}
	// вторая бронь с пожеланием переиспользует вопрос
	b2, _ := r.s.Book(ctx, wishReq(10))
	if !b2.Reused || b2.Question.ID != b.Question.ID {
		t.Errorf("вопрос не переиспользован: %+v", b2)
	}
	// статус брони следует за ответом отеля
	for _, tc := range []struct {
		answered, yes bool
		want          string
	}{{false, false, WishWaiting}, {true, true, WishConfirmed}, {true, false, WishRefused}} {
		r.hotelier.answered, r.hotelier.yes = tc.answered, tc.yes
		if v, err := r.s.Booking(ctx, b.ID); err != nil || v.WishStatus != tc.want {
			t.Errorf("%+v: %v %v", tc, v.WishStatus, err)
		}
	}
	// пожелание не из предложенных
	bad := wishReq(10)
	bad.MinFloor = 2
	if _, err := r.s.Book(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("чужое пожелание: %v", err)
	}
	// без пожелания вопрос не создаётся
	r2 := newRig(t)
	nb, err := r2.s.Book(ctx, BookRequest{SessionID: "s1", HotelID: 1, CategoryID: 20})
	if err != nil || nb.WishStatus != "" || len(r2.hotelier.confirm) != 0 {
		t.Errorf("%+v %v", nb, err)
	}
	// отель уже ответил «нет»: статус известен сразу, вопрос не создаётся
	r3 := newRig(t)
	r3.hotelier.answered = true
	if rb, _ := r3.s.Book(ctx, wishReq(10)); rb.WishStatus != WishRefused || rb.Question != nil {
		t.Errorf("отказ отеля: %+v", rb)
	}
}

// Неизвестная сессия (истекла, сброс БД) — ErrNotFound (HTTP 404), а не пустой ответ или 500;
// NewSession создаёт сессию, и с ней те же вызовы работают.
func TestUnknownSessionIsNotFound(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	calls := map[string]func() error{
		"шортлист": func() error { _, err := r.s.Shortlist(ctx, "gone"); return err },
		"добавить": func() error { _, err := r.s.AddToShortlist(ctx, "gone", 1); return err },
		"убрать":   func() error { _, err := r.s.RemoveFromShortlist(ctx, "gone", 1); return err },
		"сравнение": func() error {
			_, err := r.s.Compare(ctx, CompareRequest{SessionID: "gone", HotelIDs: []int64{1, 2}})
			return err
		},
		"сравнение с желаниями": func() error {
			_, err := r.s.Compare(ctx, CompareRequest{SessionID: "gone", HotelIDs: []int64{1, 2}, Wishes: []domain.Desire{{Key: "sea_view"}}})
			return err
		},
		"контракт": func() error {
			_, err := r.s.Contract(ctx, ContractRequest{SessionID: "gone", HotelID: 1, CategoryID: 10})
			return err
		},
		"спросить отель": func() error {
			_, err := r.s.AskHotel(ctx, AskRequest{SessionID: "gone", HotelID: 1, Key: "sea_view"})
			return err
		},
		"бронь": func() error {
			_, err := r.s.Book(ctx, BookRequest{SessionID: "gone", HotelID: 1, CategoryID: 10})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: ожидали ErrNotFound, получили %v", name, err)
		}
	}
	if _, ok := r.sessions["gone"]; ok {
		t.Error("неизвестная сессия не должна создаваться как побочный эффект")
	}
	id, err := r.s.NewSession(ctx)
	if err != nil || id == "" {
		t.Fatalf("NewSession: %q %v", id, err)
	}
	if list, err := r.s.AddToShortlist(ctx, id, 1); err != nil || len(list) != 1 {
		t.Errorf("новая сессия: %v %v", list, err)
	}
}

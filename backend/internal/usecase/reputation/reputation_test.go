package reputation

import (
	"context"
	"math"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

type fakeStore struct {
	claims    []Claim
	found     bool
	trust     float64
	saved     []float64
	refreshed int
	trusts    map[domain.Author]float64
}

func (f *fakeStore) Trusts(context.Context) (map[domain.Author]float64, error) { return f.trusts, nil }

func (f *fakeStore) AuthorClaims(context.Context, domain.Author) ([]Claim, error) {
	return f.claims, nil
}
func (f *fakeStore) Trust(context.Context, domain.Author) (int, int, float64, bool, error) {
	if !f.found {
		return 0, 0, domain.DefaultTrust, false, nil
	}
	return 0, 0, f.trust, true, nil
}
func (f *fakeStore) SaveTrust(_ context.Context, _ domain.Author, _, _ int, trust float64) error {
	f.saved = append(f.saved, trust)
	f.found, f.trust = true, trust
	return nil
}
func (f *fakeStore) RefreshFacts(context.Context, domain.Author) error { f.refreshed++; return nil }

func registry(t *testing.T) domain.Registry {
	reg, err := domain.NewRegistry([]domain.Characteristic{{
		Key: domain.SeaViewKey, Title: "Вид", Level: domain.LevelFacade, ValueType: domain.ValueRange, ValueField: "min_floor", TTLDays: 365,
		Category: &domain.CategoryRule{Values: []string{"full", "none"}, ValueField: "view", Target: "full", FavorableValues: []string{"full"}},
	}, {
		Key: "pets_allowed", Title: "Питомцы", Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 180,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func claim(entity, view string, calcView string, calcR float64) Claim {
	mine := domain.Evidence{EntityType: domain.EntityRoomCategory, EntityID: entity, Key: domain.SeaViewKey, Claimed: map[string]any{"view": view},
		AuthorType: domain.AuthorHotelier, AuthorID: "5", Reliability: 0.8}
	calc := domain.Evidence{EntityType: domain.EntityRoomCategory, EntityID: entity, Key: domain.SeaViewKey, Claimed: map[string]any{"view": calcView},
		AuthorType: domain.AuthorTool, AuthorID: domain.RoomProjectionMethod, Reliability: calcR}
	return Claim{Evidence: mine, Others: []domain.Evidence{calc}}
}

// Репутация считается заново по всем утверждениям: пересчёт повторным вызовом не накапливает.
func TestUpdateRecomputesFromScratch(t *testing.T) {
	st := &fakeStore{claims: []Claim{
		claim("1", "full", "none", 0.85), // приукрашивание
		claim("2", "full", "full", 0.85), // подтверждено
		claim("3", "full", "none", 0.6),  // судья слабый — не считается
	}}
	svc := &Service{Registry: registry(t), Store: st}
	a := domain.HotelierAuthor(5)
	for k := 0; k < 3; k++ {
		u, err := svc.Update(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		if u.Agree != 1 || u.Total != 2 || math.Abs(u.After-0.5) > 1e-9 {
			t.Fatalf("прогон %d: %+v", k, u)
		}
	}
	if st.refreshed != 0 {
		t.Errorf("репутация осталась 0,5 — факты пересобирать не нужно: %d", st.refreshed)
	}
}

func TestUpdateDropsTrustAndRefreshesFacts(t *testing.T) {
	st := &fakeStore{claims: []Claim{claim("1", "full", "none", 0.85)}}
	u, err := (&Service{Registry: registry(t), Store: st}).Update(context.Background(), domain.HotelierAuthor(5))
	if err != nil {
		t.Fatal(err)
	}
	if u.Before != domain.DefaultTrust || math.Abs(u.After-1.0/3) > 1e-9 || st.refreshed != 1 || len(u.Judged) != 1 {
		t.Errorf("%+v, пересборок %d", u, st.refreshed)
	}
}

// Нечего сравнивать — запись не создаётся, новичок остаётся новичком.
func TestUpdateWithoutJudgeKeepsNewcomer(t *testing.T) {
	st := &fakeStore{claims: []Claim{claim("1", "full", "none", 0.6)}}
	u, err := (&Service{Registry: registry(t), Store: st}).Update(context.Background(), domain.HotelierAuthor(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.saved) != 0 || u.After != domain.DefaultTrust || u.Total != 0 {
		t.Errorf("%+v, записей %d", u, len(st.saved))
	}
}

type fakeSurveys struct{ reliable, total int }

func (f fakeSurveys) GuestSurveyCounts(context.Context, int64) (int, int, error) {
	return f.reliable, f.total, nil
}

// Репутация гостя: (добросовестных + 1) / (анкет + 2). Ленивый (небрежная анкета) падает ниже порога сигнала,
// внимательный растёт; новичок без анкет остаётся на 0,5. Пересборка фактов — только при изменении веса.
func TestUpdateGuest(t *testing.T) {
	cases := []struct {
		name            string
		reliable, total int
		want            float64
		refreshed       int
	}{
		{"внимательный", 1, 1, 2.0 / 3, 1},
		{"ленивый", 0, 1, 1.0 / 3, 1},
		{"без анкет", 0, 0, 0.5, 0},
	}
	for _, c := range cases {
		st := &fakeStore{}
		s := &Service{Registry: registry(t), Store: st, Guests: fakeSurveys{c.reliable, c.total}}
		u, err := s.UpdateGuest(context.Background(), 9)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(u.After-c.want) > 1e-9 || st.refreshed != c.refreshed {
			t.Errorf("%s: репутация %.3f (want %.3f), пересборок %d (want %d)", c.name, u.After, c.want, st.refreshed, c.refreshed)
		}
		if u.Author.Type != domain.AuthorGuest || u.Author.ID != "9" {
			t.Errorf("%s: автор %+v", c.name, u.Author)
		}
	}
}

// 156 после «нет»: судья — факт без автора (расчёт «видно» против трёх надёжных гостей «закрыто»), он говорит «none»,
// и честное «нет» отельера засчитывается: репутация растёт. Раньше судьёй был только расчёт, и она падала.
func TestRecalculateJudgeIsFactWithoutAuthor(t *testing.T) {
	guest := func(id string) domain.Evidence {
		return domain.Evidence{EntityType: domain.EntityRoomCategory, EntityID: "1", Key: domain.SeaViewKey, Claimed: map[string]any{"view": "none"},
			AuthorType: domain.AuthorGuest, AuthorID: id, Reliability: 0.7}
	}
	c := claim("1", "none", "full", 0.85)
	c.Others = append(c.Others, guest("1"), guest("2"), guest("3"))
	st := &fakeStore{claims: []Claim{c}, trusts: map[domain.Author]float64{
		{Type: domain.AuthorGuest, ID: "1"}: 2.0 / 3, {Type: domain.AuthorGuest, ID: "2"}: 2.0 / 3, {Type: domain.AuthorGuest, ID: "3"}: 2.0 / 3}}
	u, err := (&Service{Registry: registry(t), Store: st}).Recalculate(context.Background(), domain.HotelierAuthor(5))
	if err != nil {
		t.Fatal(err)
	}
	if u.Agree != 1 || u.Total != 1 || math.Abs(u.After-2.0/3) > 1e-9 || !u.Changed() {
		t.Errorf("%+v", u)
	}
	// Recalculate факты не пересобирает: пересборку делает вызывающий один раз после записи свидетельства.
	if st.refreshed != 0 {
		t.Errorf("пересборок %d", st.refreshed)
	}
}

// Без свидетельств других авторов судьи нет: утверждение не считается, новичок остаётся новичком.
func TestRecalculateWithoutOtherAuthorsHasNoJudge(t *testing.T) {
	c := claim("1", "full", "none", 0.85)
	c.Others = nil
	st := &fakeStore{claims: []Claim{c}}
	u, err := (&Service{Registry: registry(t), Store: st}).Recalculate(context.Background(), domain.HotelierAuthor(5))
	if err != nil {
		t.Fatal(err)
	}
	if u.Total != 0 || u.After != domain.DefaultTrust || len(st.saved) != 0 {
		t.Errorf("%+v, записей %d", u, len(st.saved))
	}
}

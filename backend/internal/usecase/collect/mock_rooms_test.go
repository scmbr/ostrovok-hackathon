package collect

import (
	"reflect"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

func facade(id int64, levels, viewFrom int) MockFacade {
	return MockFacade{ID: id, FacesStreet: id%2 == 0, Levels: levels, ViewFromFloor: viewFrom}
}

func target(id int64, coastal bool, fs ...MockFacade) RoomMockTarget {
	return RoomMockTarget{HotelID: id, Name: "Hotel", Categories: []string{"Standard", "Superior", "Suite"}, Coastal: coastal, Facades: fs}
}

func TestMockRoomsDeterministicAndConsistent(t *testing.T) {
	tg := target(42, false, facade(1, 6, 0), facade(2, 5, 0), facade(3, 8, 0))
	a, b := MockRooms(tg, false), MockRooms(tg, false)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("мок должен быть детерминирован")
	}
	if len(a) != 3 {
		t.Fatalf("категорий %d, ждали 3 из карточки", len(a))
	}
	levels := map[int64]int{1: 6, 2: 5, 3: 8}
	for _, c := range a {
		if c.Rooms() <= 0 || len(c.Placements) == 0 || len(c.Placements) > 3 {
			t.Errorf("%s: номеров %d, размещений %d", c.Name, c.Rooms(), len(c.Placements))
		}
		for _, p := range c.Placements {
			if p.Rooms < 1 || p.FloorFrom < 1 || p.FloorTo < p.FloorFrom || p.FloorTo > levels[p.FacadeID] {
				t.Errorf("%s: размещение %+v вне этажности фасада %d", c.Name, p, levels[p.FacadeID])
			}
		}
	}
	if reflect.DeepEqual(MockRooms(target(43, false, facade(1, 6, 0), facade(2, 5, 0), facade(3, 8, 0)), false), a) {
		t.Error("у разных отелей мок должен различаться")
	}
	if got := MockRooms(target(1, true), false); got != nil {
		t.Errorf("без фасадов категорий нет: %v", got)
	}
}

func seaView(in []RoomsInput) *RoomsInput {
	for i := range in {
		if domain.TitleClaimsSeaView(in[i].Name) {
			return &in[i]
		}
	}
	return nil
}

// viewAt строит ValueAt по фасадам мока, как это делает расчёт по фактам sea_view.
func viewAt(fs []MockFacade) domain.ValueAt {
	return func(id int64, floor int) (string, bool) {
		for _, f := range fs {
			if f.ID == id {
				if f.ViewFromFloor > 0 && floor >= f.ViewFromFloor {
					return "full", true
				}
				return "none", true
			}
		}
		return "", false
	}
}

func TestMockRoomsHonestSeaViewIsGuaranteed(t *testing.T) {
	fs := []MockFacade{facade(1, 8, 3), facade(2, 6, 0), facade(3, 10, 5)}
	tg := target(7, true, fs...)
	cat := seaView(MockRooms(tg, false))
	if cat == nil {
		t.Fatal("у берегового отеля с видом должна быть «Sea View» / «Vista mar»")
	}
	a := domain.AssessCategory(cat.Rooms(), placed(cat), viewAt(fs), "full")
	if !a.Guarantee {
		t.Errorf("честная «Sea View» должна быть гарантированной: %+v", a)
	}
	if cat.Persona != PersonaHonest {
		t.Errorf("персона %q", cat.Persona)
	}
	// У небереговых отелей и береговых без вида «Sea View» нет, если они не приукрашивающие.
	if seaView(MockRooms(target(7, false, fs...), false)) != nil {
		t.Error("«Sea View» у небереговых отелей не создаётся")
	}
	if seaView(MockRooms(target(8, true, facade(1, 6, 0)), false)) != nil {
		t.Error("честный береговой отель без вида не называет категорию «Sea View»")
	}
}

func TestMockRoomsBeautifierSeaViewHasNoView(t *testing.T) {
	fs := []MockFacade{facade(1, 6, 0), facade(2, 5, 0)}
	cat := seaView(MockRooms(target(9, true, fs...), true))
	if cat == nil {
		t.Fatal("приукрашивающий отель называет категорию «Sea View»")
	}
	a := domain.AssessCategory(cat.Rooms(), placed(cat), viewAt(fs), "full")
	if a.Probability != 0 || a.Guarantee {
		t.Errorf("море закрыто со всех фасадов, а вероятность %.2f", a.Probability)
	}
	if cat.Persona != PersonaEmbellishing {
		t.Errorf("персона %q", cat.Persona)
	}
}

func placed(c *RoomsInput) []domain.Placement { return c.Placements }

func TestPickBeautifiers(t *testing.T) {
	var ts []RoomMockTarget
	for id := int64(1); id <= 10; id++ {
		ts = append(ts, target(id, true, facade(id*10, 6, 0)))
	}
	ts = append(ts, target(11, true, facade(110, 6, 3)))  // вид есть — не кандидат
	ts = append(ts, target(12, false, facade(120, 6, 0))) // не береговой — не кандидат
	ts = append(ts, target(13, true))                     // без фасадов — не кандидат
	got := PickBeautifiers(ts, BeautifiersCount)
	if len(got) != BeautifiersCount || got[11] || got[12] || got[13] {
		t.Fatalf("приукрашивающие: %v", got)
	}
	// Порядок отелей не влияет на выбор.
	rev := make([]RoomMockTarget, len(ts))
	for i := range ts {
		rev[len(ts)-1-i] = ts[i]
	}
	if !reflect.DeepEqual(PickBeautifiers(rev, BeautifiersCount), got) {
		t.Error("выбор зависит от порядка отелей")
	}
}

func TestSplitSumsToRooms(t *testing.T) {
	tg := target(5, true, facade(1, 12, 4), facade(2, 12, 6), facade(3, 12, 0))
	for id := int64(1); id < 50; id++ {
		tg.HotelID = id
		for _, c := range MockRooms(tg, false) {
			sum := 0
			for _, p := range c.Placements {
				sum += p.Rooms
			}
			if sum != c.Rooms() {
				t.Fatalf("отель %d, %s: сумма %d != %d", id, c.Name, sum, c.Rooms())
			}
		}
	}
}

func TestMockRoomsDensityPerFloor(t *testing.T) {
	tg := target(0, true, facade(1, 12, 4), facade(2, 12, 6), facade(3, 12, 0), facade(4, 3, 0))
	for id := int64(1); id < 80; id++ {
		tg.HotelID = id
		for _, c := range MockRooms(tg, false) {
			for _, p := range c.Placements {
				span, levels := p.FloorTo-p.FloorFrom+1, map[int64]int{1: 12, 2: 12, 3: 12, 4: 3}[p.FacadeID]
				// Больше roomsPerFloorMax на этаж допустимо только если диапазон уже занял все этажи, доступные категории.
				if p.Rooms > span*roomsPerFloorMax && span < levels && p.FloorFrom > 1 && p.FloorTo < levels {
					t.Fatalf("отель %d, %s: %d номеров на %d этажах", id, c.Name, p.Rooms, span)
				}
			}
		}
	}
}

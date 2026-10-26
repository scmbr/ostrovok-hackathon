package collect

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Персоны имитированного отельера (hotelier_questions.persona, details свидетельств размещения).
const (
	PersonaHonest       = "honest"
	PersonaEmbellishing = "embellishing"
)

// BeautifiersCount — у стольких береговых отелей без вида «Sea View» размещена на фасадах без вида на море:
// материал для демо приукрашивающего отельера.
const BeautifiersCount = 3

// roomsPerFloorMax — больше номеров категории на одном этаже одного фасада мок не размещает (если хватает этажей).
const roomsPerFloorMax = 8

var (
	seaViewNames = []string{"Sea View", "Vista mar"}
	// roomsPerCategory — диапазон числа номеров в категории: [от, до].
	roomsPerCategory = map[string][2]int{
		"Standard": {24, 60}, "Superior": {14, 30}, "Deluxe": {8, 20}, "Family": {6, 16}, "Suite": {4, 10},
	}
)

// MockFacade — фасад отеля с этажностью и этажом начала вида на море (по расчётному факту sea_view).
type MockFacade struct {
	ID          int64
	FacesStreet bool
	Levels      int
	// ViewFromFloor — этаж, с которого фасад видит море; 0 — не видит.
	ViewFromFloor int
}

// RoomMockTarget — демо-отель, для которого генерируются категории номеров.
type RoomMockTarget struct {
	HotelID    int64
	Name       string
	Categories []string // из мок-карточки
	Coastal    bool
	Facades    []MockFacade
}

// RoomsInput — категория номеров с размещением, которое «сообщил» имитированный отельер.
type RoomsInput struct {
	Name       string
	Persona    string
	Placements []domain.Placement
}

// Rooms — число номеров категории: сумма по размещениям.
func (r RoomsInput) Rooms() int {
	n := 0
	for _, p := range r.Placements {
		n += p.Rooms
	}
	return n
}

// RoomMockStore — хранилище мока категорий.
type RoomMockStore interface {
	// RoomMockTargets возвращает демо-отели с фасадами, для которых посчитан sea_view.
	RoomMockTargets(ctx context.Context, flt domain.HotelFilter) ([]RoomMockTarget, error)
	// SaveHotelRooms заменяет категории отеля из набора вместе с размещением и свидетельствами отельера.
	SaveHotelRooms(ctx context.Context, hotelID int64, in []RoomsInput) error
}

// RoomsMocker генерирует категории и размещение номеров демо-отелей.
type RoomsMocker struct {
	Store RoomMockStore
}

// RoomMockResult — итог генерации.
type RoomMockResult struct {
	Hotels, Categories, Placements int
	SeaViewHonest                  int      // категории «Sea View» с видом по расчёту
	Beautifiers                    []string // отели, у которых «Sea View» размещена на фасадах без вида
}

// Run генерирует категории демо-отелей (по flt.HotelID — одного). Приукрашивающие отели выбираются среди всех
// демо-отелей, а не среди отфильтрованных: результат по отелю не зависит от фильтра.
func (m *RoomsMocker) Run(ctx context.Context, flt domain.HotelFilter) (RoomMockResult, error) {
	var res RoomMockResult
	all, err := m.Store.RoomMockTargets(ctx, domain.HotelFilter{RegionID: flt.RegionID})
	if err != nil {
		return res, fmt.Errorf("демо-отели: %w", err)
	}
	beautify := PickBeautifiers(all, BeautifiersCount)
	found := false
	for _, t := range all {
		if flt.HotelID != 0 && t.HotelID != flt.HotelID {
			continue
		}
		found = true
		in := MockRooms(t, beautify[t.HotelID])
		if len(in) == 0 {
			continue
		}
		if err := m.Store.SaveHotelRooms(ctx, t.HotelID, in); err != nil {
			return res, fmt.Errorf("отель %d: %w", t.HotelID, err)
		}
		res.Hotels++
		res.Categories += len(in)
		for _, c := range in {
			res.Placements += len(c.Placements)
			if domain.TitleClaimsSeaView(c.Name) {
				if beautify[t.HotelID] {
					res.Beautifiers = append(res.Beautifiers, t.Name)
				} else {
					res.SeaViewHonest++
				}
			}
		}
	}
	if flt.HotelID != 0 && !found {
		return res, fmt.Errorf("отель %d не демо-отель или у него нет фасадов с расчётом sea_view", flt.HotelID)
	}
	return res, nil
}

// PickBeautifiers выбирает n береговых отелей без вида на море ни с одного фасада. Порядок — по хешу id,
// поэтому выбор детерминирован и не зависит от порядка отелей.
func PickBeautifiers(ts []RoomMockTarget, n int) map[int64]bool {
	var cands []RoomMockTarget
	for _, t := range ts {
		if t.Coastal && len(usable(t.Facades)) > 0 && len(viewFacades(t.Facades)) == 0 {
			cands = append(cands, t)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		hi, hj := idHash(cands[i].HotelID), idHash(cands[j].HotelID)
		if hi != hj {
			return hi < hj
		}
		return cands[i].HotelID < cands[j].HotelID
	})
	out := map[int64]bool{}
	for i := 0; i < len(cands) && i < n; i++ {
		out[cands[i].HotelID] = true
	}
	return out
}

func idHash(id int64) uint32 { return uint32(id) * 2654435761 }

func usable(fs []MockFacade) []MockFacade {
	var out []MockFacade
	for _, f := range fs {
		if f.Levels >= 1 {
			out = append(out, f)
		}
	}
	return out
}

func viewFacades(fs []MockFacade) []MockFacade {
	var out []MockFacade
	for _, f := range usable(fs) {
		if f.ViewFromFloor > 0 {
			out = append(out, f)
		}
	}
	return out
}

// MockRooms строит категории отеля и размещение номеров; результат зависит только от отеля и признака beautify.
//   - обычные категории (из мок-карточки) размещаются случайно по 1–3 фасадам;
//   - у берегового отеля с видом по расчёту добавляется честная «Sea View» / «Vista mar»: на фасадах с видом,
//     начиная с этажа начала вида;
//   - при beautify «Sea View» размещена на фасадах, где расчёт вида не даёт ни на одном этаже.
//
// Сумма номеров по размещениям категории — её число номеров (RoomsInput.Rooms).
func MockRooms(t RoomMockTarget, beautify bool) []RoomsInput {
	facades := usable(t.Facades)
	if len(facades) == 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(t.HotelID))
	persona := PersonaHonest
	if beautify {
		persona = PersonaEmbellishing
	}

	names := t.Categories
	if len(names) == 0 {
		names = []string{"Standard", "Superior"}
	}
	var out []RoomsInput
	for _, name := range names {
		if domain.TitleClaimsSeaView(name) {
			continue // «Sea View» добавляется ниже по результату расчёта, а не по карточке
		}
		rooms := rangeRooms(rng, name)
		out = append(out, RoomsInput{Name: name, Persona: persona, Placements: place(rng, rooms, randomFacades(rng, facades, 3), false)})
	}

	seaName := seaViewNames[t.HotelID%int64(len(seaViewNames))]
	switch view := viewFacades(t.Facades); {
	case t.Coastal && len(view) > 0:
		out = append(out, RoomsInput{Name: seaName, Persona: persona, Placements: place(rng, 6+rng.Intn(10), randomFacades(rng, view, 2), true)})
	case beautify:
		out = append(out, RoomsInput{Name: seaName, Persona: persona, Placements: place(rng, 8+rng.Intn(7), randomFacades(rng, facades, 2), false)})
	}
	return out
}

func rangeRooms(rng *rand.Rand, name string) int {
	r, ok := roomsPerCategory[name]
	if !ok {
		r = [2]int{8, 20}
	}
	return r[0] + rng.Intn(r[1]-r[0]+1)
}

// randomFacades выбирает от 1 до max фасадов в случайном порядке.
func randomFacades(rng *rand.Rand, fs []MockFacade, max int) []MockFacade {
	k := 1 + rng.Intn(min(max, len(fs)))
	out := make([]MockFacade, 0, k)
	for _, i := range rng.Perm(len(fs))[:k] {
		out = append(out, fs[i])
	}
	return out
}

// place делит rooms номеров между фасадами (каждому не меньше одного) и выбирает диапазон этажей.
// withView — этажи только от начала вида на фасаде: honest-категория «Sea View» видит море на всех номерах.
func place(rng *rand.Rand, rooms int, fs []MockFacade, withView bool) []domain.Placement {
	if rooms < len(fs) {
		fs = fs[:rooms]
	}
	parts := split(rng, rooms, len(fs))
	out := make([]domain.Placement, len(fs))
	for i, f := range fs {
		lo := 1
		if withView {
			lo = f.ViewFromFloor
		}
		from := lo + rng.Intn(f.Levels-lo+1)
		to := from + rng.Intn(f.Levels-from+1)
		// Не больше roomsPerFloorMax номеров на этаж фасада, пока хватает этажей: диапазон растёт вверх, затем вниз.
		for need := (parts[i] + roomsPerFloorMax - 1) / roomsPerFloorMax; to-from+1 < need && (to < f.Levels || from > lo); {
			if to < f.Levels {
				to++
			} else {
				from--
			}
		}
		out[i] = domain.Placement{FacadeID: f.ID, FloorFrom: from, FloorTo: to, Rooms: parts[i]}
	}
	return out
}

// split делит n на k положительных частей случайно; сумма частей равна n.
func split(rng *rand.Rand, n, k int) []int {
	parts := make([]int, k)
	for i := range parts {
		parts[i] = 1
	}
	for left := n - k; left > 0; left-- {
		parts[rng.Intn(k)]++
	}
	return parts
}

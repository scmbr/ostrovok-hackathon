package domain

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Категория номеров: Островок продаёт категории, а факты у нас по фасадам и этажам. Размещение номеров
// (сообщает отельер) переносит факты фасад × этаж на категорию как вероятность, гарантию и пожелание.

// Ключи и сущности размещения номеров.
const (
	EntityRoomCategory = "room_category"
	RoomPlacementKey   = "room_placement"
	// RoomProjectionMethod — метод расчётного свидетельства категории (проекция фасадных фактов по размещению).
	RoomProjectionMethod = "room_projection@1"
	// HotelierBaseReliability — база надёжности свидетельства отельера (раздел «Источники и логика правды»).
	HotelierBaseReliability = 0.8
	// SourceHotelier — источник свидетельства отельера (evidence.source).
	SourceHotelier = "hotelier"
	// RoomPlacementTTLDays — срок годности размещения, сообщённого отельером.
	RoomPlacementTTLDays = 365
)

// HotelierAuthor — автор-отельер; идентификатор — id отеля (кабинет отельера привязан к одному отелю).
func HotelierAuthor(hotelID int64) Author {
	return Author{Type: AuthorHotelier, ID: strconv.FormatInt(hotelID, 10)}
}

// Placement — часть категории: сколько номеров на фасаде в диапазоне этажей. Номера делятся по этажам поровну.
type Placement struct {
	FacadeID           int64
	FloorFrom, FloorTo int
	Rooms              int
}

// ValueAt возвращает значение характеристики у фасада на этаже; false — значение неизвестно.
type ValueAt func(facadeID int64, floor int) (string, bool)

// Wish — пожелание к брони: «фасад X, этаж N+». Отель подтверждает его, если номера под ним есть.
type Wish struct {
	FacadeID int64
	// MinFloor — этаж, с которого значение выполняется на всех номерах фасада в категории; 0 — этаж не важен.
	MinFloor int
	Rooms    float64 // номеров категории под пожеланием
	Share    float64 // доля номеров категории под пожеланием
}

// CategoryAssessment — значение характеристики в категории номеров.
type CategoryAssessment struct {
	Rooms int
	// Shares — доля номеров категории с каждым значением. Сумма меньше 1, если часть номеров не размещена
	// или значение у их фасада неизвестно (Unknown).
	Shares  map[string]float64
	Unknown float64
	// Target — искомое значение; Probability — доля номеров с ним (шанс получить его без пожелания).
	Target      string
	Probability float64
	// Guarantee — все номера категории имеют искомое значение.
	Guarantee bool
	// Wish — лучшее пожелание для искомого значения; nil, если искомого не бывает или оно и так гарантировано.
	Wish *Wish
}

// AssessCategory считает для категории из rooms номеров с размещениями ps, какова доля номеров с каждым
// значением характеристики at, вероятность искомого значения target, гарантию и лучшее пожелание.
// Номера делятся по этажам диапазона поровну. Не размещённые номера и фасады без значения — Unknown:
// гарантии при них нет.
func AssessCategory(rooms int, ps []Placement, at ValueAt, target string) CategoryAssessment {
	out := CategoryAssessment{Rooms: rooms, Shares: map[string]float64{}, Target: target}
	if rooms <= 0 {
		return out
	}
	total := float64(rooms)
	counts := map[string]float64{}
	known := 0.0
	allTarget := true
	// perFacade: номера на фасаде по этажам, для пожелания.
	perFacade := map[int64]map[int]float64{}
	for _, p := range ps {
		floors := p.FloorTo - p.FloorFrom + 1
		if floors <= 0 || p.Rooms <= 0 {
			continue
		}
		per := float64(p.Rooms) / float64(floors)
		for f := p.FloorFrom; f <= p.FloorTo; f++ {
			v, ok := at(p.FacadeID, f)
			if !ok {
				allTarget = false
				continue
			}
			counts[v] += per
			known += per
			if v != target {
				allTarget = false
			}
			if perFacade[p.FacadeID] == nil {
				perFacade[p.FacadeID] = map[int]float64{}
			}
			perFacade[p.FacadeID][f] += per
		}
	}
	for v, n := range counts {
		out.Shares[v] = n / total
	}
	out.Unknown = math.Max(0, (total-known)/total)
	out.Probability = out.Shares[target]
	// Гарантия: все номера размещены и все значения известные и искомые. Допуск — на дробное деление номеров.
	out.Guarantee = allTarget && out.Unknown < 1e-9 && out.Probability > 1-1e-9
	if !out.Guarantee {
		out.Wish = bestWish(perFacade, ps, at, target, total)
	}
	return out
}

// bestWish выбирает фасад, где пожелание «фасад, этаж N+» покрывает больше всего номеров категории.
// Этаж N — наименьший, начиная с которого на фасаде все номера категории имеют искомое значение.
func bestWish(perFacade map[int64]map[int]float64, ps []Placement, at ValueAt, target string, total float64) *Wish {
	var best *Wish
	ids := make([]int64, 0, len(perFacade))
	for id := range perFacade {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		byFloor := perFacade[id]
		floors := make([]int, 0, len(byFloor))
		for f := range byFloor {
			floors = append(floors, f)
		}
		sort.Ints(floors)
		// Идём с верхнего этажа вниз, пока значение искомое: так находится N+.
		rooms := 0.0
		minFloor := 0
		for i := len(floors) - 1; i >= 0; i-- {
			if v, _ := at(id, floors[i]); v != target {
				break
			}
			rooms += byFloor[floors[i]]
			minFloor = floors[i]
		}
		if rooms == 0 {
			continue
		}
		if minFloor == floors[0] {
			minFloor = 0 // условие по этажу не нужно: все номера фасада в категории подходят
		}
		if best == nil || rooms > best.Rooms+1e-9 {
			best = &Wish{FacadeID: id, MinFloor: minFloor, Rooms: rooms, Share: rooms / total}
		}
	}
	return best
}

// seaViewTitles — названия категорий, в которых отель обещает вид на море. Сравнение без регистра.
var seaViewTitles = []string{"sea view", "seaview", "vista mar", "vista al mar", "вид на море"}

// TitleClaimsSeaView сообщает, обещает ли название категории вид на море («Sea View», «Vista mar»).
// Название — утверждение отеля, а не факт: его проверяет расчёт по фасадам.
func TitleClaimsSeaView(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, t := range seaViewTitles {
		if strings.Contains(n, t) {
			return true
		}
	}
	return false
}

// RoomCategory — категория номеров отеля с размещением, как её хранит БД.
type RoomCategory struct {
	ID         int64
	HotelID    int64
	Name       string
	Rooms      int // rooms_count: сумма номеров по размещениям
	Placements []StoredPlacement
}

// StoredPlacement — размещение из БД со ссылкой на свидетельство отельера.
type StoredPlacement struct {
	Placement
	ID         int64
	Source     string
	Confidence float64
	EvidenceID int64
}

// Placed возвращает размещения для расчётов.
func (c RoomCategory) Placed() []Placement {
	out := make([]Placement, len(c.Placements))
	for i, p := range c.Placements {
		out[i] = p.Placement
	}
	return out
}

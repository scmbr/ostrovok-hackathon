package search

import (
	"context"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// fakePoi — ближайшие заведения по отелям: kind → заведение; радиус учитывается, как в SQL.
type fakePoi struct {
	byHotel map[int64]map[string]domain.NearbyPoi
}

func (f fakePoi) NearestPoi(_ context.Context, id int64, kinds []string, radiusM float64) (map[string]domain.NearbyPoi, error) {
	out := map[string]domain.NearbyPoi{}
	for _, k := range kinds {
		if n, ok := f.byHotel[id][k]; ok && n.DistM <= radiusM {
			out[k] = n
		}
	}
	return out, nil
}

func poiService(t *testing.T) *Service {
	t.Helper()
	s, _ := newTestService(t)
	s.Registry = s.Registry.WithPoi(testPoi(t))
	s.Poi = fakePoi{byHotel: map[int64]map[string]domain.NearbyPoi{
		1: {"nightclub": {Kind: "nightclub", Name: "Razz", DistM: 180}, "bar": {Kind: "bar", DistM: 90}, "pharmacy": {Kind: "pharmacy", DistM: 120}},
		2: {"nightclub": {Kind: "nightclub", DistM: 280}},
		3: {"pub": {Kind: "pub", DistM: 150}},
	}}
	return s
}

func testPoi(t *testing.T) domain.PoiDict {
	t.Helper()
	d, err := domain.NewPoiDict([]domain.PoiKind{
		{ID: "nightclub", Kinds: []string{"nightclub"}, Label: "клуб", Title: "Клуб рядом", TitleMet: "клуб рядом", TitleUnmet: "клуба рядом нет"},
		{ID: "nightlife", Kinds: []string{"nightclub", "bar", "pub"}, Label: "заведение", Labels: map[string]string{"nightclub": "клуб", "bar": "бар", "pub": "паб"},
			Title: "Бары и клубы рядом", TitleMet: "бары и клубы рядом", TitleUnmet: "баров и клубов рядом нет"},
		{ID: "pharmacy", Kinds: []string{"pharmacy"}, Label: "аптека", Title: "Аптека рядом", TitleMet: "аптека рядом", TitleUnmet: "аптеки рядом нет"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSearchNearPoiRanksByDistanceAndExplains(t *testing.T) {
	s := poiService(t)
	res, err := s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "near_poi.nightclub"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hotels) != 3 || res.Hotels[0].HotelID != 1 || res.Hotels[1].HotelID != 2 || res.Hotels[2].HotelID != 3 {
		t.Fatalf("порядок выдачи: ближе клуб — выше: %+v", res.Hotels)
	}
	e := func(i int) domain.WishEval { return res.Hotels[i].Best.Evals[0] }
	if e(0).Status != domain.StatusMatch || e(0).Nearby == nil || e(0).Nearby.Name != "Razz" || e(0).Nearby.Label != "клуб" {
		t.Errorf("отель 1: %+v", e(0))
	}
	if e(1).Status != domain.StatusNear {
		t.Errorf("отель 2 (280 м): статус %s, ожидалось «на границе»", e(1).Status)
	}
	if e(2).Status != domain.StatusMismatch || !strings.Contains(e(2).Caveat, "в OSM не нашли в 300 м") {
		t.Errorf("отель 3: %+v", e(2))
	}
	c, _ := s.Registry.Get("near_poi.nightclub")
	if got := domain.ExplainWish(c, e(0)); !strings.Contains(got, "клуб в 180 м по прямой") {
		t.Errorf("объяснение: %q", got)
	}
}

// «Ночная жизнь»: берётся ближайшее из клуба, бара и паба, в объяснении — конкретный тип.
func TestSearchNearPoiGroupNamesConcreteKind(t *testing.T) {
	s := poiService(t)
	res, err := s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "near_poi.nightlife"}}})
	if err != nil {
		t.Fatal(err)
	}
	byHotel := map[int64]domain.WishEval{}
	for _, h := range res.Hotels {
		byHotel[h.HotelID] = h.Best.Evals[0]
	}
	if n := byHotel[1].Nearby; n == nil || n.Kind != "bar" || n.Label != "бар" || n.DistM != 90 {
		t.Errorf("отель 1: ближайший — бар в 90 м, получено %+v", byHotel[1].Nearby)
	}
	if n := byHotel[3].Nearby; n == nil || n.Label != "паб" {
		t.Errorf("отель 3: ближайший — паб, получено %+v", byHotel[3].Nearby)
	}
}

// Группы выдачи подхватывают near_poi сами: отель без клуба уходит в блок «всё, кроме клуба» с человеческим заголовком.
func TestSearchNearPoiGroups(t *testing.T) {
	s := poiService(t)
	res, err := s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "near_poi.nightclub"}, {Key: "walk_to_beach", Value: 10.0}}})
	if err != nil {
		t.Fatal(err)
	}
	var block *domain.Group
	for i := range res.Groups {
		if res.Groups[i].Kind == domain.GroupCompromise && res.Groups[i].FailedKey == "near_poi.nightclub" {
			block = &res.Groups[i]
		}
	}
	if block == nil {
		t.Fatalf("нет блока «всё, кроме клуба»: %+v", res.Groups)
	}
	if title := domain.GroupTitle(s.Registry, *block); !strings.Contains(title, "клуба рядом нет") {
		t.Errorf("заголовок блока: %q", title)
	}
}

func TestNearbyEndpointData(t *testing.T) {
	s := poiService(t)
	got, err := s.Nearby(context.Background(), 1, []string{"nightclub", "pharmacy", "nightlife"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got[0].Found || got[0].Poi.DistM != 180 || !got[1].Found || got[1].Poi.Label != "аптека" || got[2].Poi.Label != "бар" {
		t.Errorf("ответ: %+v", got)
	}
	none, err := s.Nearby(context.Background(), 2, []string{"pharmacy"})
	if err != nil || len(none) != 1 || none[0].Found {
		t.Errorf("аптеки нет в радиусе: %+v %v", none, err)
	}
	if _, err := s.Nearby(context.Background(), 1, []string{"zoo"}); err == nil {
		t.Error("неизвестный тип должен отклоняться")
	}
}

// Без желаний near_poi источник заведений не вызывается: запрос поиска к БД не меняется.
func TestSearchWithoutNearPoiDoesNotTouchPoi(t *testing.T) {
	s := poiService(t)
	s.Poi = failingPoi{t}
	if _, err := s.Search(context.Background(), domain.Query{Wishes: []domain.Desire{{Key: "walk_to_beach", Value: 10.0}}}); err != nil {
		t.Fatal(err)
	}
}

type failingPoi struct{ t *testing.T }

func (f failingPoi) NearestPoi(context.Context, int64, []string, float64) (map[string]domain.NearbyPoi, error) {
	f.t.Fatal("NearestPoi вызван без желаний near_poi")
	return nil, nil
}

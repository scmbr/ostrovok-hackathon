package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

type fakeSearch struct {
	res  search.Result
	card search.HotelCard
	sims []search.SimilarHotel
	err  error
	got  domain.Query
	w    map[string]float64
}

func (f *fakeSearch) Search(_ context.Context, q domain.Query) (search.Result, error) {
	f.got = q
	return f.res, f.err
}
func (f *fakeSearch) Hotel(context.Context, int64) (search.HotelCard, error) { return f.card, f.err }
func (f *fakeSearch) Similar(_ context.Context, _ int64, _ int, w map[string]float64) ([]search.SimilarHotel, error) {
	f.w = w
	return f.sims, f.err
}

func searchRouter(f *fakeSearch) http.Handler {
	return newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithSearch(f))
}

func doReq(h http.Handler, method, url, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, url, strings.NewReader(body)))
	return rec
}

func TestSearchEndpoint(t *testing.T) {
	f := &fakeSearch{res: search.Result{Considered: 1, Hotels: []domain.HotelResult{{
		HotelID: 3, Name: "Arts", Card: domain.Card{Stars: 5, PriceEUR: 200},
		Best: domain.Variant{Name: "Sea View", PriceEUR: 260, Score: 0.9, Evals: []domain.WishEval{{
			Key: "sea_view", Importance: domain.Must, Status: domain.StatusChance, PBase: 0.3, PWish: 0.7, P: 0.7,
			Wish: &domain.VariantWish{FacadeID: 1, AzimuthDeg: 90, MinFloor: 4, Probability: 0.7}}}},
	}}}}
	rec := doReq(searchRouter(f), "POST", "/api/search",
		`{"wishes":[{"key":"sea_view","importance":"must","min_floor":3}],"filters":{"price_max":150},"session_id":"s1"}`)
	if rec.Code != 200 {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body)
	}
	if f.got.SessionID != "s1" || f.got.Filters.PriceMax != 150 || f.got.Wishes[0].MinFloor != 3 {
		t.Errorf("запрос не разобран: %+v", f.got)
	}
	var out searchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	w := out.Hotels[0].Best.Wishes[0]
	if w.Status != "chance" || w.BookingWish == nil || w.BookingWish.Text != "восточная сторона, этаж 4+" || w.PBase != 0.3 || w.PWish != 0.7 {
		t.Errorf("объяснение: %+v", w)
	}
	if out.Hotels[0].Violations == nil || out.Hotels[0].Alternative != nil {
		t.Errorf("нарушения должны быть пустым массивом, альтернативы нет: %+v", out.Hotels[0])
	}
}

func TestSearchEndpointErrors(t *testing.T) {
	cases := []struct {
		name   string
		f      *fakeSearch
		method string
		url    string
		body   string
		code   int
	}{
		{"проверка запроса", &fakeSearch{err: &search.ValidationError{Msg: "нет такой характеристики"}}, "POST", "/api/search", `{"wishes":[{"key":"x"}]}`, 400},
		{"неизвестное поле", &fakeSearch{}, "POST", "/api/search", `{"wishez":[]}`, 400},
		{"не json", &fakeSearch{}, "POST", "/api/search", `не json`, 400},
		{"сбой", &fakeSearch{err: context.DeadlineExceeded}, "POST", "/api/search", `{"wishes":[{"key":"sea_view"}]}`, 500},
		{"нет отеля", &fakeSearch{err: search.ErrNotFound}, "GET", "/api/hotels/9", "", 404},
		{"плохой id", &fakeSearch{}, "GET", "/api/hotels/abc", "", 400},
		{"плохие веса", &fakeSearch{}, "GET", "/api/hotels/9/similar?weights=sea_view", "", 400},
		{"плохой limit", &fakeSearch{}, "GET", "/api/hotels/9/similar?limit=0", "", 400},
	}
	for _, c := range cases {
		if rec := doReq(searchRouter(c.f), c.method, c.url, c.body); rec.Code != c.code {
			t.Errorf("%s: статус %d, ждали %d (%s)", c.name, rec.Code, c.code, rec.Body)
		}
	}
}

func TestSimilarEndpointWeightsAndDifferences(t *testing.T) {
	f := &fakeSearch{sims: []search.SimilarHotel{{
		Candidate: search.Candidate{ID: 4, Name: "Twin"}, Distance: 0.2,
		Dims: []domain.Dim{{Key: "walk_to_beach", Distance: 0.5, Weight: 2}, {Key: "pets_allowed", Distance: 0, Weight: 1}},
	}}}
	rec := doReq(searchRouter(f), "GET", "/api/hotels/3/similar?limit=2&weights=walk_to_beach:2,sea_view:3", "")
	if rec.Code != 200 || f.w["walk_to_beach"] != 2 || f.w["sea_view"] != 3 {
		t.Fatalf("статус %d, веса %v: %s", rec.Code, f.w, rec.Body)
	}
	var out struct {
		Similar []similarHotelDTO `json:"similar"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Similar) != 1 || len(out.Similar[0].Differences) != 1 || out.Similar[0].Differences[0].Key != "walk_to_beach" {
		t.Errorf("отличия: %+v", out.Similar)
	}
}

func TestHotelCardEndpoint(t *testing.T) {
	f := &fakeSearch{card: search.HotelCard{Candidate: search.Candidate{ID: 3, Name: "Arts", Card: domain.Card{Stars: 5}}}}
	rec := doReq(searchRouter(f), "GET", "/api/hotels/3", "")
	var out hotelCardDTO
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Name != "Arts" || out.Facts == nil {
		t.Fatalf("карточка: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"rooms":null`) {
		t.Errorf("у отеля без категорий rooms = null: %s", rec.Body)
	}
}

func TestSearchRoutesAbsentWithoutService(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil)
	if rec := doReq(h, "POST", "/api/search", `{}`); rec.Code == 200 {
		t.Error("без сервиса поиска маршрута нет")
	}
}

func (f *fakeSearch) Nearby(context.Context, int64, []string) ([]search.NearbyType, error) {
	return nil, f.err
}

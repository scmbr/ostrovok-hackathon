package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

type fakeRooms struct {
	h   rooms.HotelRooms
	err error
}

func (f fakeRooms) Hotel(context.Context, int64) (rooms.HotelRooms, error) { return f.h, f.err }

func TestSideRU(t *testing.T) {
	for az, want := range map[float64]string{0: "северная", 44: "северо-восточная", 90: "восточная", 181: "южная", 270: "западная", 359: "северная"} {
		if got := sideRU(az); got != want {
			t.Errorf("азимут %v: %s, ждали %s", az, got, want)
		}
	}
	if wishText("восточная", 4) != "восточная сторона, этаж 4+" || wishText("южная", 0) != "южная сторона" {
		t.Error("текст пожелания")
	}
}

func TestRoomsEndpoint(t *testing.T) {
	h := rooms.HotelRooms{
		HotelID: 7,
		Facades: []rooms.FacadeInfo{{ID: 1, AzimuthDeg: 92}},
		Categories: []rooms.CategoryView{{
			Category: domain.RoomCategory{ID: 5, Name: "Standard", Rooms: 20,
				Placements: []domain.StoredPlacement{{Placement: domain.Placement{FacadeID: 1, FloorFrom: 2, FloorTo: 6, Rooms: 6}}}},
			Characteristics: []rooms.CharacteristicView{{
				Key: "sea_view", Title: "Вид на море",
				Assessment: domain.CategoryAssessment{Target: "full", Probability: 0.2449, Shares: map[string]float64{"full": 0.2449},
					Wish: &domain.Wish{FacadeID: 1, MinFloor: 4, Rooms: 3.6, Share: 0.18}},
			}},
		}},
	}
	router := newRouter(fstest.MapFS{}, domain.Registry{}, nil, fakeRooms{h: h}, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hotels/7/rooms", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got roomsDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	ch := got.Categories[0].Characteristics[0]
	if got.HotelID != 7 || ch.Probability != 0.24 || ch.Wish == nil || ch.Wish.Text != "восточная сторона, этаж 4+" {
		t.Errorf("ответ: %+v", got)
	}

	for name, tt := range map[string]struct {
		url  string
		src  RoomsSource
		code int
	}{
		"нет категорий": {"/api/hotels/7/rooms", fakeRooms{err: rooms.ErrNotFound}, http.StatusNotFound},
		"плохой id":     {"/api/hotels/x/rooms", fakeRooms{}, http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		newRouter(fstest.MapFS{}, domain.Registry{}, nil, tt.src, nil).ServeHTTP(rec, httptest.NewRequest("GET", tt.url, nil))
		if rec.Code != tt.code {
			t.Errorf("%s: status %d, ждали %d", name, rec.Code, tt.code)
		}
	}
}

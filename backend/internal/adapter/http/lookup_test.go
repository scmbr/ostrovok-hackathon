package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/lookup"
)

type fakeLookup struct {
	got string
	res lookup.Result
	err error
}

func (f *fakeLookup) Lookup(_ context.Context, q string) (lookup.Result, error) {
	f.got = q
	return f.res, f.err
}

func lookupRouter(f *fakeLookup) http.Handler {
	return newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithHotelLookup(f))
}

func TestHotelLookupEndpoint(t *testing.T) {
	f := &fakeLookup{res: lookup.Result{Matches: []lookup.Match{
		{Hotel: lookup.Hotel{ID: 7, Name: "Hotel Arts", District: "Vila Olímpica", Card: domain.Card{Stars: 5, PriceEUR: 300}}, Score: 1, By: lookup.MatchID},
	}}}
	rec := doReq(lookupRouter(f), "GET", "/api/hotels/lookup?q=Hotel+Arts", "")
	if rec.Code != http.StatusOK || f.got != "Hotel Arts" {
		t.Fatalf("код %d, запрос %q: %s", rec.Code, f.got, rec.Body)
	}
	var out struct {
		Query   string `json:"query"`
		Results []struct {
			ID    int64   `json:"id"`
			Name  string  `json:"name"`
			Score float64 `json:"score"`
			Match string  `json:"match"`
			Card  struct {
				Stars int `json:"stars"`
			} `json:"card"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].ID != 7 || out.Results[0].Score != 1 || out.Results[0].Match != "id" || out.Results[0].Card.Stars != 5 {
		t.Errorf("ответ: %+v", out)
	}
}

// Пустой результат — пустой массив, а не null: фронтенд читает длину.
func TestHotelLookupEmptyAndErrors(t *testing.T) {
	rec := doReq(lookupRouter(&fakeLookup{}), "GET", "/api/hotels/lookup?q=zzz", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"results":[]`) {
		t.Errorf("пустой результат: %d %s", rec.Code, rec.Body)
	}
	if rec := doReq(lookupRouter(&fakeLookup{err: errors.New("db")}), "GET", "/api/hotels/lookup?q=a", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("ошибка хранилища: код %d", rec.Code)
	}
	rec = doReq(lookupRouter(&fakeLookup{res: lookup.Result{Unknown: "foobar"}}), "GET", "/api/hotels/lookup?q=hotel+foobar", "")
	if !strings.Contains(rec.Body.String(), `"unknown":"foobar"`) || !strings.Contains(rec.Body.String(), `"results":[]`) {
		t.Errorf("неизвестный отель: %s", rec.Body)
	}
	long := "/api/hotels/lookup?q=" + strings.Repeat("a", lookup.MaxQueryLen+1)
	if rec := doReq(lookupRouter(&fakeLookup{}), "GET", long, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("длинный запрос: код %d", rec.Code)
	}
	if rec := doReq(lookupRouter(&fakeLookup{}), "POST", "/api/hotels/lookup", ""); rec.Code == http.StatusOK {
		t.Errorf("POST не должен обслуживаться: %d", rec.Code)
	}
}

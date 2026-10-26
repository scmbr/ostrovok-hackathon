package openmeteo

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paulmach/orb"
)

func TestDailyMergesWeatherAndSea(t *testing.T) {
	hours := make([]string, 0, 48)
	sst := make([]string, 0, 48)
	for d := 1; d <= 2; d++ {
		for h := 0; h < 24; h++ {
			hours = append(hours, fmt.Sprintf(`"2024-07-0%dT%02d:00"`, d, h))
			// 1 июля есть вся сутки (24 °C), 2 июля — только 6 часов (меньше половины суток: не считается).
			if d == 1 || h < 6 {
				sst = append(sst, "24")
			} else {
				sst = append(sst, "null")
			}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("latitude") != "41.38" || r.URL.Query().Get("start_date") != "2024-07-01" {
			t.Errorf("запрос: %s", r.URL.RawQuery)
		}
		if strings.HasPrefix(r.URL.Path, "/marine") {
			fmt.Fprintf(w, `{"hourly":{"time":[%s],"sea_surface_temperature":[%s]}}`, strings.Join(hours, ","), strings.Join(sst, ","))
			return
		}
		// 2 июля: солнце неизвестно, осадки 3 мм; 3600 с солнца = 1 час.
		fmt.Fprint(w, `{"daily":{"time":["2024-07-01","2024-07-02"],"temperature_2m_mean":[26.5,null],
			"temperature_2m_max":[30,null],"temperature_2m_min":[23,null],"precipitation_sum":[0,3],"sunshine_duration":[7200,null]}}`)
	}))
	defer srv.Close()

	c := &Client{ArchiveURL: srv.URL + "/archive", MarineURL: srv.URL + "/marine", HTTP: srv.Client()}
	days, err := c.Daily(context.Background(), orb.Point{2.19, 41.38},
		time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 7, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("суток %d", len(days))
	}
	d1, d2 := days[0], days[1]
	if d1.MeanC != 26.5 || d1.PrecipMM != 0 || d1.SunHours != 2 || d1.SeaC != 24 {
		t.Errorf("1 июля: %+v", d1)
	}
	if !math.IsNaN(d2.MeanC) || d2.PrecipMM != 3 || !math.IsNaN(d2.SunHours) || !math.IsNaN(d2.SeaC) {
		t.Errorf("2 июля: %+v", d2)
	}
}

func TestDailyRetriesAndFailsOnClientError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"daily":{"time":[]},"hourly":{"time":[]}}`)
	}))
	defer srv.Close()
	c := &Client{ArchiveURL: srv.URL, MarineURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Daily(context.Background(), orb.Point{2, 41}, time.Now(), time.Now()); err != nil || calls != 3 {
		t.Errorf("повтор после 429: err %v, запросов %d, want 3 (повтор + море)", err, calls)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"reason":"bad"}`)
	}))
	defer bad.Close()
	c = &Client{ArchiveURL: bad.URL, MarineURL: bad.URL, HTTP: bad.Client()}
	if _, err := c.Daily(context.Background(), orb.Point{2, 41}, time.Now(), time.Now()); err == nil {
		t.Error("ошибка клиента 400 не вернулась")
	}
}

package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/dialog"
)

func TestHealth(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %s", got)
	}
}

func TestSPAFallback(t *testing.T) {
	h := newRouter(fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}, domain.Registry{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/hotel/42", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "app") {
		t.Errorf("fallback: %d %q", rec.Code, rec.Body.String())
	}
}

func TestAPINotFoundIsJSON(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("api 404: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestCharacteristicsEndpoint(t *testing.T) {
	reg, err := domain.NewRegistry([]domain.Characteristic{{
		Key: "night_pharmacy", Title: "Ночная аптека", Description: "d", Level: domain.LevelHotel, ValueType: domain.ValueBool,
		TTLDays: 90, FavorableValues: []string{"true"},
		Strategies: []domain.Strategy{{
			Tool: "night-services", Params: map[string]float64{"radius_m": 300}, Lists: map[string][]string{"kinds": {"pharmacy"}},
			BaseReliability: 0.6, IndependenceGroup: "osm_pois",
		}},
		AskHotelier: "Есть ли рядом ночная аптека?",
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newRouter(fstest.MapFS{}, reg, nil, nil, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/api/characteristics", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var got struct {
		Characteristics []struct {
			Key        string `json:"key"`
			ValueField string `json:"value_field"`
			TTLDays    int    `json:"ttl_days"`
			Conflict   struct {
				Steps int     `json:"steps"`
				Theta float64 `json:"theta"`
			} `json:"conflict"`
			Strategies []struct {
				Tool            string         `json:"tool"`
				Params          map[string]any `json:"params"`
				BaseReliability float64        `json:"base_reliability"`
			} `json:"strategies"`
			AskHotelier string `json:"ask_hotelier"`
		} `json:"characteristics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Characteristics) != 1 {
		t.Fatalf("ответ: %v %s", err, rec.Body.String())
	}
	c := got.Characteristics[0]
	if c.Key != "night_pharmacy" || c.ValueField != "value" || c.TTLDays != 90 || c.Conflict.Steps != 1 || c.Conflict.Theta != 1 {
		t.Errorf("характеристика: %+v", c)
	}
	s := c.Strategies[0]
	kinds, _ := s.Params["kinds"].([]any)
	if s.Tool != "night-services" || s.BaseReliability != 0.6 || s.Params["radius_m"] != 300.0 || len(kinds) != 1 || kinds[0] != "pharmacy" {
		t.Errorf("стратегия: %+v", s)
	}

	// Пустой реестр — пустой список, а не null: клиент не должен проверять на nil.
	rec = httptest.NewRecorder()
	newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/api/characteristics", nil))
	if !strings.Contains(rec.Body.String(), `"characteristics":[]`) {
		t.Errorf("пустой реестр: %s", rec.Body.String())
	}
}

func TestDialogExamplesRoute(t *testing.T) {
	ex, err := dialog.Examples()
	if err != nil {
		t.Fatal(err)
	}
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithDialogExamples(ex))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dialog/examples", nil))
	var body struct {
		Examples []exampleDTO `json:"examples"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Examples) != 3 || body.Examples[0].Text == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

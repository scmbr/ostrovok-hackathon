package overpass

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Устаревший снимок зеркала отклоняется, и запрос уходит на следующее зеркало.
func TestQuerySkipsStaleMirror(t *testing.T) {
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"osm3s":{"timestamp_osm_base":"2026-05-31T22:37:44Z"},"elements":[]}`))
	}))
	defer stale.Close()
	fresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"osm3s":{"timestamp_osm_base":"2026-10-26T11:20:20Z"},"elements":[{"type":"node","id":1}]}`))
	}))
	defer fresh.Close()
	c := &Client{
		Endpoints:  []string{stale.URL, fresh.URL},
		HTTP:       http.DefaultClient,
		MaxDataAge: DefaultMaxDataAge,
		Now:        func() time.Time { return time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC) },
	}
	body, err := c.query(context.Background(), "test", "x")
	if err != nil {
		t.Fatal(err)
	}
	els, err := decode(body)
	if err != nil || len(els) != 1 {
		t.Fatalf("взят устаревший ответ: %s (%v)", body, err)
	}
}

func TestQueryWithoutTimestampIsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"elements":[]}`)) }))
	defer srv.Close()
	c := &Client{Endpoints: []string{srv.URL}, HTTP: http.DefaultClient, MaxDataAge: DefaultMaxDataAge}
	if _, err := c.query(context.Background(), "test", "x"); err != nil {
		t.Fatal(err)
	}
}

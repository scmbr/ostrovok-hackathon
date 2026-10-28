package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
)

// stubFlow — GuestFlow, у которого сессий нет; остальные методы не вызываются (nil-интерфейс паникует).
type stubFlow struct{ GuestFlow }

func (stubFlow) Shortlist(context.Context, string) ([]booking.Shown, error) {
	return nil, fmt.Errorf("%w: %w", booking.ErrNotFound, booking.ErrSessionNotFound)
}
func (stubFlow) NewSession(context.Context) (string, error) { return "new-id", nil }

func TestUnknownSessionShortlistIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/x/shortlist", nil)
	req.SetPathValue("id", "x")
	handleShortlist(stubFlow{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("статус %d, нужен 404: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"code":"session_not_found"`) {
		t.Errorf("нет кода session_not_found: %s", rec.Body)
	}
}

func TestNewSessionEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	handleNewSession(stubFlow{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sessions", nil))
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"session_id":"new-id"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

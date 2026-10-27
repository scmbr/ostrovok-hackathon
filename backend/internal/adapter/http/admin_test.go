package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/admin"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/explain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

type fakeAdmin struct{ hotel int64 }

func (f *fakeAdmin) Runs(_ context.Context, name string, hotel int64, _ int) ([]admin.Run, error) {
	f.hotel = hotel
	return []admin.Run{{Agent: name, SessionID: "s1", HotelID: hotel, Mode: "cache", Steps: 5, Refusals: 2, StartedAt: time.Now(), EndedAt: time.Now(), Title: "обзор"}}, nil
}

func (f *fakeAdmin) RunSteps(_ context.Context, id string) ([]agent.TraceStep, error) {
	if id == "none" {
		return nil, nil
	}
	return []agent.TraceStep{{Agent: "collector", SessionID: id, HotelID: 161, Mode: "cache", Step: 1, Decision: "обзор фактов"},
		{Agent: "collector", SessionID: id, Mode: "cache", Step: 2, Decision: "отказ: инструмент уже запускали", Tool: "run_tool"}}, nil
}

func (f *fakeAdmin) Conflicts(context.Context, admin.ConflictFilter) ([]admin.ConflictView, error) {
	return []admin.ConflictView{{Conflict: admin.Conflict{ID: 1, EntityType: "room_category", EntityID: "697", Key: "sea_view", Status: "open", HotelID: 156},
		Title: "Вид на море", Theta: 0.3, ThetaLowered: true, ThetaReason: "θ = 0.3 понижен",
		Sides: []admin.Side{{Value: "none", Weight: 4.5}, {Value: "full", Weight: 2.8}}, Question: &admin.QuestionRef{ID: 744, Status: "open", Format: "confirm"}}}, nil
}
func (f *fakeAdmin) Tasks(context.Context, int64, int) ([]admin.Task, error) {
	return []admin.Task{{ID: 1, HotelID: 156, Key: "sea_view", Reason: "3 гостя", Status: "done", Outcome: "confirmed"}}, nil
}
func (f *fakeAdmin) StaleFacts(context.Context, int64, int) ([]admin.StaleFact, error) {
	return nil, nil
}

type fakeExplain struct{}

func (fakeExplain) Hotel(context.Context, int64) ([]explain.FactView, error) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	es := []domain.Evidence{
		{ID: 1, EntityType: "facade", EntityID: "1", Key: "noise_night", Claimed: map[string]any{"level": "loud"}, Source: "bcn_noise",
			AuthorType: domain.AuthorTool, AuthorID: "bcn", Reliability: 0.85, ObservedAt: t0},
		{ID: 2, EntityType: "facade", EntityID: "1", Key: "noise_night", Claimed: map[string]any{"level": "quiet"}, Source: "hotelier",
			AuthorType: domain.AuthorHotelier, AuthorID: "5", Reliability: 0.8, ObservedAt: t0, Details: map[string]any{"comment": "двойные окна"}},
	}
	x, _ := domain.ExplainFact(es, domain.TruthContext{Now: t0, Rules: map[string]domain.TruthParams{
		"noise_night": {Kind: domain.TruthEnum, ValueField: "level", Order: []string{"quiet", "moderate", "loud"}, FavorableValues: []string{"quiet"}}}})
	return []explain.FactView{{EntityType: "facade", EntityID: "1", Key: "noise_night", Title: "Тишина ночью", Explanation: x}}, nil
}

func TestAdminRunsAndSteps(t *testing.T) {
	f := &fakeAdmin{}
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithAdmin(f, fakeExplain{}))
	rec := do(h, "GET", "/api/admin/runs?agent=collector&hotel_id=161", "")
	var runs struct{ Runs []runDTO }
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil || rec.Code != 200 || len(runs.Runs) != 1 || runs.Runs[0].Refusals != 2 || f.hotel != 161 {
		t.Fatalf("прогоны: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/admin/runs?hotel_id=abc", ""); rec.Code != 400 {
		t.Errorf("hotel_id: %d", rec.Code)
	}
	rec = do(h, "GET", "/api/admin/runs/s1", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"refusal":true`) || !strings.Contains(rec.Body.String(), `"refusal":false`) {
		t.Errorf("шаги: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/admin/runs/none", ""); rec.Code != 404 {
		t.Errorf("нет прогона: %d", rec.Code)
	}
}

func TestAdminConflicts(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithAdmin(&fakeAdmin{}, fakeExplain{}))
	rec := do(h, "GET", "/api/admin/conflicts?status=open", "")
	var out struct {
		Conflicts []adminConflictDTO
		Tasks     []taskDTO
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body)
	}
	c := out.Conflicts[0]
	if !c.ThetaLowered || len(c.Sides) != 2 || c.Sides[0].Value != "none" || c.Question == nil || c.Question.ID != 744 || len(out.Tasks) != 1 {
		t.Errorf("конфликт: %+v", c)
	}
	if rec := do(h, "GET", "/api/admin/conflicts?status=zzz", ""); rec.Code != 400 {
		t.Errorf("status: %d", rec.Code)
	}
}

// Разбор факта отдаёт цепочку надёжности по шагам и сводку; вклад нулевого веса помечен.
func TestAdminHotelFactsChain(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithAdmin(&fakeAdmin{}, fakeExplain{}))
	rec := do(h, "GET", "/api/admin/hotels/5/facts", "")
	var out struct{ Facts []explainedFactDTO }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 || len(out.Facts) != 1 {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body)
	}
	f := out.Facts[0]
	if f.Summary == "" || len(f.Evidence) != 2 {
		t.Fatalf("факт: %+v", f)
	}
	hot := f.Evidence[1]
	if hot.Chain.Base != 0.8 || hot.Chain.Asymmetry != domain.HotelierAsymmetry || hot.Chain.TrustMult != 1 || hot.Comment != "двойные окна" || hot.Role != domain.RoleAgainst {
		t.Errorf("цепочка отельера: %+v", hot)
	}
}

type fakeRegistryAdmin struct {
	draftErr error
	saveErr  error
}

func (f fakeRegistryAdmin) Draft(context.Context, string) (registry.Draft, error) {
	if f.draftErr != nil {
		return registry.Draft{}, f.draftErr
	}
	return registry.Draft{YAML: "key: night_shop\n", Key: "night_shop", Title: "Ночной магазин", Synonyms: []string{"ночной магазин"},
		Errors: []string{"night_shop: нужна проверка"}}, nil
}

func (f fakeRegistryAdmin) Save(context.Context, string) (domain.Characteristic, error) {
	return domain.Characteristic{Key: "night_shop", Title: "Ночной магазин"}, f.saveErr
}
func (f fakeRegistryAdmin) StartCompute(context.Context, string) (string, error) { return "job-1", nil }
func (f fakeRegistryAdmin) Job(id string) (registry.JobView, bool) {
	if id != "job-1" {
		return registry.JobView{}, false
	}
	return registry.JobView{ID: id, Status: registry.JobRunning, Done: 3, Total: 60}, true
}

func TestAdminCharacteristicDraftSaveCompute(t *testing.T) {
	mk := func(f fakeRegistryAdmin) *struct {
		do func(m, u, b string) (int, string)
	} {
		h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithRegistryAdmin(f), WithLLMMode(LLMModeCache, ""))
		return &struct {
			do func(m, u, b string) (int, string)
		}{func(m, u, b string) (int, string) {
			rec := do(h, m, u, b)
			return rec.Code, rec.Body.String()
		}}
	}
	ok := mk(fakeRegistryAdmin{})
	if code, body := ok.do("POST", "/api/admin/characteristics/draft", `{"text":"есть ли рядом круглосуточный магазин"}`); code != 200 ||
		!strings.Contains(body, `"mode":"cache"`) || !strings.Contains(body, `"errors":["night_shop: нужна проверка"]`) {
		t.Errorf("draft: %d %s", code, body)
	}
	if code, _ := ok.do("POST", "/api/admin/characteristics/draft", `{"text":"  "}`); code != 400 {
		t.Errorf("пустой текст: %d", code)
	}
	if code, body := mk(fakeRegistryAdmin{draftErr: fmt.Errorf("%w: night_pharmacy", registry.ErrExists)}).do("POST", "/api/admin/characteristics/draft", `{"text":"x"}`); code != 409 ||
		!strings.Contains(body, "уже есть в реестре") {
		t.Errorf("уже есть: %d %s", code, body)
	}
	if code, _ := mk(fakeRegistryAdmin{draftErr: errors.New("нет заготовки")}).do("POST", "/api/admin/characteristics/draft", `{"text":"x"}`); code != 422 {
		t.Errorf("ошибка модели: %d", code)
	}
	if code, body := ok.do("POST", "/api/admin/characteristics", `{"yaml":"key: night_shop"}`); code != 201 || !strings.Contains(body, "night_shop") {
		t.Errorf("save: %d %s", code, body)
	}
	bad := mk(fakeRegistryAdmin{saveErr: &registry.ValidationError{Problems: []string{"нет synonyms", "пересечение"}}})
	if code, body := bad.do("POST", "/api/admin/characteristics", `{"yaml":"key: night_shop"}`); code != 422 || !strings.Contains(body, `"errors":["нет synonyms","пересечение"]`) {
		t.Errorf("проверка: %d %s", code, body)
	}
	if code, body := ok.do("POST", "/api/admin/characteristics/night_shop/compute", ``); code != 202 || !strings.Contains(body, "job-1") {
		t.Errorf("compute: %d %s", code, body)
	}
	if code, body := ok.do("GET", "/api/admin/jobs/job-1", ``); code != 200 || !strings.Contains(body, `"done":3`) {
		t.Errorf("job: %d %s", code, body)
	}
	if code, _ := ok.do("GET", "/api/admin/jobs/zzz", ``); code != 404 {
		t.Errorf("нет задания: %d", code)
	}
}

func TestAdminSummary(t *testing.T) {
	h := newRouter(fstest.MapFS{}, domain.Registry{}, nil, nil, nil, WithAdmin(&fakeAdmin{}, fakeExplain{}))
	rec := do(h, "GET", "/api/admin/summary", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"question_share":0.25`) || !strings.Contains(rec.Body.String(), `"open_conflicts":3`) {
		t.Errorf("сводка: %d %s", rec.Code, rec.Body)
	}
}

func (f *fakeAdmin) Summary(context.Context) (admin.Summary, error) {
	return admin.Summary{DemoHotels: 60, Coverage: []admin.Coverage{{Key: "pets_allowed", Hotels: 30, AvgConfidence: 0.7}}, OpenConflicts: 3,
		Questions: map[string]int{"open": 5}, HotelsAsked: 15}, nil
}

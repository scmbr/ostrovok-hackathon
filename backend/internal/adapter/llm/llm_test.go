package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

func TestClientSendsChatCompletion(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"key: x"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1/", "secret", "model-1")
	got, err := c.Complete(context.Background(), registry.Request{System: "sys", User: "usr"})
	if err != nil || got != "key: x" {
		t.Fatalf("%q, %v", got, err)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer secret" || gotReq.Model != "model-1" ||
		len(gotReq.Messages) != 2 || gotReq.Messages[0].Role != "system" || gotReq.Messages[1].Content != "usr" {
		t.Errorf("запрос: %s %s %+v", gotPath, gotAuth, gotReq)
	}
}

func TestClientErrors(t *testing.T) {
	for name, tt := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"ошибка API":    {401, `{"error":{"message":"bad key"}}`, "bad key"},
		"не JSON":       {200, `<html>`, "не JSON"},
		"нет ответов":   {200, `{"choices":[]}`, "пустой ответ"},
		"ошибка прокси": {502, `gateway`, "502"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(tt.body))
		}))
		_, err := NewClient(srv.URL, "k", "m").Complete(context.Background(), registry.Request{})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, ожидалось %q", name, err, tt.want)
		}
	}
}

func TestCachedMatchesByNormalizedInput(t *testing.T) {
	c, err := NewCached()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"можно ли с собакой", "а можно ли с собакой в номер", "есть ли рядом круглосуточная аптека", "затемняющие шторы"} {
		if _, err := c.Complete(context.Background(), registry.Request{Task: registry.TaskAddCharacteristic, Key: key}); err != nil {
			t.Errorf("%q: %v", key, err)
		}
	}
	_, err = c.Complete(context.Background(), registry.Request{Task: registry.TaskAddCharacteristic, Key: "бассейн на крыше"})
	var nc *ErrNotCached
	if !errors.As(err, &nc) || len(nc.Examples) != 4 || !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Errorf("нет в кеше: %v", err)
	}
	// Подстрока слова не считается совпадением: «дособакой» — не «с собакой».
	if _, err := c.Complete(context.Background(), registry.Request{Task: registry.TaskAddCharacteristic, Key: "можно ли с собакойдом"}); err == nil {
		t.Error("совпадение должно быть по целым словам")
	}
	if _, err := c.Complete(context.Background(), registry.Request{Task: "other", Key: "можно ли с собакой"}); err == nil {
		t.Error("ответ другой задачи не должен подходить")
	}
}

func TestCachedRejectsIncompleteEntry(t *testing.T) {
	if _, err := newCached(fstest.MapFS{"cache/x.yaml": {Data: []byte("task: t\n")}}, "cache"); err == nil {
		t.Error("запись без inputs и response должна быть ошибкой")
	}
}

// Главная проверка режима кеша: заготовленные ответы проходят ту же проверку реестра, что и живые, и дают
// характеристики без нового кода (аптека — тот же инструмент night-services с kinds: [pharmacy]).
func TestCannedAnswersPassRegistryValidation(t *testing.T) {
	base, err := domain.NewRegistry([]domain.Characteristic{{
		Key: domain.NightServicesKey, Title: "t", Description: "d", Level: domain.LevelHotel, ValueType: domain.ValueEnum,
		Values: []string{"none", "basic", "rich"}, ValueField: "level", TTLDays: 90,
		Strategies: []domain.Strategy{{Tool: "night-services", BaseReliability: 0.6, IndependenceGroup: "osm_pois"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cached, err := NewCached()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		input, key string
		strategies int
	}{
		{"Есть ли рядом круглосуточная аптека?", "night_pharmacy", 1},
		{"Можно ли с собакой?", "pet_friendly", 0},
		{"Затемняющие шторы", "blackout_curtains", 0},
		{"Есть ли рядом круглосуточный магазин?", "night_shop", 1},
	} {
		adder := &registry.Adder{LLM: cached, Registry: base, Tools: compute.Specs, Parse: adapterregistry.Parse}
		res, err := adder.Add(context.Background(), tt.input)
		if err != nil {
			t.Errorf("%q: %v", tt.input, err)
			continue
		}
		c := res.Characteristic
		if c.Key != tt.key || len(c.Strategies) != tt.strategies || res.Attempts != 1 {
			t.Errorf("%q: %+v", tt.input, res)
		}
		if tt.strategies == 0 && c.AskHotelier == "" {
			t.Errorf("%s: без инструмента нужен вопрос отельеру", tt.key)
		}
		// Результат записывается в YAML и читается обратно без потерь.
		body, err := adapterregistry.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		back, err := adapterregistry.Parse(body)
		if err != nil {
			t.Fatalf("%s: повторный разбор: %v\n%s", tt.key, err, body)
		}
		if again, _ := adapterregistry.Marshal(back); string(again) != string(body) {
			t.Errorf("%s: YAML меняется при круге:\n%s\n---\n%s", tt.key, body, again)
		}
	}
}

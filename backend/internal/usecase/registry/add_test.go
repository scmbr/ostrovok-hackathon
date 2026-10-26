package registry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

// fakeLLM отдаёт ответы по очереди и запоминает запросы.
type fakeLLM struct {
	answers []string
	reqs    []Request
}

func (f *fakeLLM) Complete(_ context.Context, req Request) (string, error) {
	f.reqs = append(f.reqs, req)
	if len(f.reqs) > len(f.answers) {
		return "", errors.New("нет ответа")
	}
	return f.answers[len(f.reqs)-1], nil
}

// parseStub разбирает «YAML» тестов: ответом служит ключ характеристики, остальное берётся из шаблона valid().
func parseStub(data []byte) (domain.Characteristic, error) {
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "broken") {
		return domain.Characteristic{}, errors.New("YAML: сломан")
	}
	c := valid()
	c.Key = text
	if strings.Contains(text, "magic") {
		c.Key = "magic_char"
		c.Strategies[0].Tool = "magic-eye"
	}
	if strings.Contains(text, "liar") {
		c.Key = "liar_char"
		c.Strategies[0].BaseReliability = 0.97
		c.Strategies[0].IndependenceGroup = "my_own_group"
	}
	return c, nil
}

func registryWithNightServices(t *testing.T) domain.Registry {
	t.Helper()
	reg, err := domain.NewRegistry([]domain.Characteristic{{
		Key: domain.NightServicesKey, Title: "t", Description: "d", Level: domain.LevelHotel, ValueType: domain.ValueEnum,
		Values: []string{"none", "basic", "rich"}, ValueField: "level", TTLDays: 90,
		Strategies: []domain.Strategy{{Tool: "night-services", BaseReliability: 0.6, IndependenceGroup: "osm_pois"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func newAdder(t *testing.T, llm LLM) *Adder {
	return &Adder{LLM: llm, Registry: registryWithNightServices(t), Tools: compute.Specs, Parse: parseStub}
}

func TestAddAcceptsValidAnswerAndAsksWithCatalog(t *testing.T) {
	llm := &fakeLLM{answers: []string{"```yaml\nnight_pharmacy\n```"}}
	res, err := newAdder(t, llm).Add(context.Background(), "  Есть ли рядом круглосуточная аптека? ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Characteristic.Key != "night_pharmacy" || res.Attempts != 1 {
		t.Errorf("результат: %+v", res)
	}
	req := llm.reqs[0]
	if req.Task != TaskAddCharacteristic || req.Key != "есть ли рядом круглосуточная аптека" {
		t.Errorf("запрос: %+v", req)
	}
	// Каталог инструментов — в промпте: LLM выбирает только из него.
	for _, want := range []string{"night-services", "параметр-набор kinds", "strategy bcn", "walk_to_beach", "ask_hotelier"} {
		if !strings.Contains(req.System, want) {
			t.Errorf("в промпте нет %q", want)
		}
	}
	if !strings.Contains(req.User, "круглосуточная аптека") {
		t.Errorf("описание не передано: %q", req.User)
	}
}

// Выдуманный инструмент — не ответ: LLM получает ошибки и одну попытку исправиться.
func TestAddRetriesOnceWithValidationErrors(t *testing.T) {
	llm := &fakeLLM{answers: []string{"magic", "fixed_char"}}
	res, err := newAdder(t, llm).Add(context.Background(), "что-нибудь")
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 2 || res.Characteristic.Key != "fixed_char" {
		t.Errorf("результат: %+v", res)
	}
	second := llm.reqs[1].User
	if !strings.Contains(second, "инструмента нет") || !strings.Contains(second, "magic") {
		t.Errorf("во втором запросе нет ошибок и прошлого ответа: %q", second)
	}
}

func TestAddFailsAfterSecondInvalidAnswer(t *testing.T) {
	llm := &fakeLLM{answers: []string{"magic", "magic again"}}
	_, err := newAdder(t, llm).Add(context.Background(), "что-нибудь")
	if err == nil || !strings.Contains(err.Error(), "не прошёл проверку после 2 попыток") || !strings.Contains(err.Error(), "инструмента нет") {
		t.Errorf("ошибка: %v", err)
	}
	if len(llm.reqs) != 2 {
		t.Errorf("запросов %d, want 2", len(llm.reqs))
	}
}

func TestAddBrokenYAMLIsRetried(t *testing.T) {
	llm := &fakeLLM{answers: []string{"broken", "second_char"}}
	res, err := newAdder(t, llm).Add(context.Background(), "x")
	if err != nil || res.Attempts != 2 {
		t.Fatalf("%+v, %v", res, err)
	}
	if !strings.Contains(llm.reqs[1].User, "сломан") {
		t.Errorf("ошибка разбора не передана: %q", llm.reqs[1].User)
	}
}

// Надёжность источника LLM не выбирает: для инструмента, который уже есть в реестре, база и группа берутся оттуда.
func TestAddInheritsReliabilityFromRegistry(t *testing.T) {
	res, err := newAdder(t, &fakeLLM{answers: []string{"liar"}}).Add(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	s := res.Characteristic.Strategies[0]
	if s.BaseReliability != 0.6 || s.IndependenceGroup != "osm_pois" {
		t.Errorf("стратегия: %+v", s)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "взяты из реестра") {
		t.Errorf("заметки: %v", res.Notes)
	}
}

func TestAddRejectsDuplicateKeyAndEmptyDescription(t *testing.T) {
	a := newAdder(t, &fakeLLM{answers: []string{domain.NightServicesKey, domain.NightServicesKey}})
	if _, err := a.Add(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "уже есть в реестре") {
		t.Errorf("дубль ключа: %v", err)
	}
	if _, err := a.Add(context.Background(), "  "); err == nil {
		t.Error("пустое описание должно быть ошибкой")
	}
}

func TestAddPropagatesLLMError(t *testing.T) {
	_, err := newAdder(t, &fakeLLM{}).Add(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "нет ответа") {
		t.Errorf("ошибка LLM: %v", err)
	}
}

func TestExtractYAMLAndNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"key: a":               "key: a",
		"```yaml\nkey: a\n```": "key: a",
		"Вот ответ:\n```\nkey: a\n```\nГотово": "key: a",
	} {
		if got := ExtractYAML(in); got != want {
			t.Errorf("ExtractYAML(%q) = %q, want %q", in, got, want)
		}
	}
	if got := NormalizeInput("  Можно ли с СОБАКОЙ?! Ёлка-палка "); got != "можно ли с собакой елка палка" {
		t.Errorf("NormalizeInput = %q", got)
	}
}

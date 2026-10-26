package registry

import (
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

// valid — образец корректной характеристики с универсальным инструментом.
func valid() domain.Characteristic {
	return domain.Characteristic{
		Key: "night_pharmacy", Title: "Ночная аптека", Description: "Есть ли рядом аптека, открытая ночью",
		Level: domain.LevelHotel, ValueType: domain.ValueBool, ValueField: "available", TTLDays: 90,
		FavorableValues: []string{"true"},
		Strategies: []domain.Strategy{{
			Tool: "night-services", Params: map[string]float64{"radius_m": 300},
			Lists:           map[string][]string{"kinds": {"pharmacy"}},
			BaseReliability: 0.6, IndependenceGroup: "osm_pois",
		}},
		AskHotelier: "Есть ли рядом ночная аптека?",
	}
}

func check(t *testing.T, c domain.Characteristic) error {
	t.Helper()
	return ValidateOne(domain.Registry{}, c, compute.Specs)
}

func TestValidateAcceptsGoodCharacteristic(t *testing.T) {
	if err := check(t, valid()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	for name, tt := range map[string]struct {
		mutate func(*domain.Characteristic)
		want   string
	}{
		"нет инструмента": {func(c *domain.Characteristic) { c.Strategies[0].Tool = "magic-eye" }, "инструмента нет"},
		"инструмент не считает ключ": {func(c *domain.Characteristic) {
			c.Key, c.Strategies[0].Tool = "sea_view_2", "sea-view"
			c.Strategies[0].Params, c.Strategies[0].Lists = nil, nil
		}, "инструмент считает только sea_view"},
		"неизвестный параметр":         {func(c *domain.Characteristic) { c.Strategies[0].Params["speed"] = 1 }, "неизвестный параметр speed"},
		"неизвестный набор":            {func(c *domain.Characteristic) { c.Strategies[0].Lists["brands"] = []string{"x"} }, "неизвестный параметр-набор brands"},
		"плохой вид заведения":         {func(c *domain.Characteristic) { c.Strategies[0].Lists["kinds"] = []string{"hospital"} }, `содержит "hospital"`},
		"надёжность вне (0;1)":         {func(c *domain.Characteristic) { c.Strategies[0].BaseReliability = 1 }, "base_reliability"},
		"нет группы":                   {func(c *domain.Characteristic) { c.Strategies[0].IndependenceGroup = "" }, "independence_group"},
		"нет срока годности":           {func(c *domain.Characteristic) { c.TTLDays = 0 }, "ttl_days"},
		"плохой ключ":                  {func(c *domain.Characteristic) { c.Key = "Night Pharmacy" }, "snake_case"},
		"плохой уровень":               {func(c *domain.Characteristic) { c.Level = "room" }, "level"},
		"район для отельного":          {func(c *domain.Characteristic) { c.Level = domain.LevelDistrict }, "считает по отелям"},
		"плохой тип":                   {func(c *domain.Characteristic) { c.ValueType = "blob" }, "value_type"},
		"выгодное не из values":        {func(c *domain.Characteristic) { c.FavorableValues = []string{"maybe"} }, "favorable_values"},
		"values у bool":                {func(c *domain.Characteristic) { c.Values = []string{"a", "b"} }, "bool values не задаются"},
		"число без порога":             {func(c *domain.Characteristic) { c.ValueType, c.FavorableValues = domain.ValueNumber, nil }, "significant_diff"},
		"порог у не-числа":             {func(c *domain.Characteristic) { c.SignificantDiff = 5 }, "significant_diff задаётся только у number"},
		"ступени у неупорядоченного":   {func(c *domain.Characteristic) { c.ConflictSteps = 2 }, "ordered enum"},
		"нет ни стратегий, ни вопроса": {func(c *domain.Characteristic) { c.Strategies, c.AskHotelier = nil, "" }, "значение неоткуда взять"},
		"повтор стратегии": {func(c *domain.Characteristic) {
			c.Strategies = append(c.Strategies, c.Strategies[0])
		}, "уже использованы"},
	} {
		c := valid()
		tt.mutate(&c)
		err := check(t, c)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: ошибка %v, ожидалось %q", name, err, tt.want)
		}
	}
}

func TestValidateEnumRules(t *testing.T) {
	c := valid()
	c.ValueType, c.Values, c.Ordered, c.ConflictSteps = domain.ValueEnum, []string{"none", "some"}, true, 2
	c.FavorableValues, c.ValueField = []string{"some"}, "level"
	if err := check(t, c); err == nil || !strings.Contains(err.Error(), "больше числа ступеней") {
		t.Errorf("conflict_steps 2 при двух значениях: %v", err)
	}
	c.ConflictSteps = 1
	if err := check(t, c); err != nil {
		t.Errorf("корректный enum: %v", err)
	}
	c.Values = []string{"none"}
	if err := check(t, c); err == nil || !strings.Contains(err.Error(), "не меньше двух") {
		t.Errorf("enum из одного значения: %v", err)
	}
}

func TestValidateVariantRequiredWhereToolHasVariants(t *testing.T) {
	c := domain.Characteristic{
		Key: "noise_night", Title: "Тишина", Description: "d", Level: domain.LevelFacade, ValueType: domain.ValueEnum,
		Values: []string{"quiet", "moderate", "loud"}, Ordered: true, TTLDays: 365,
		Strategies: []domain.Strategy{{Tool: "noise-night", BaseReliability: 0.8, IndependenceGroup: "g"}},
	}
	if err := check(t, c); err == nil || !strings.Contains(err.Error(), "допустимо bcn, osm") {
		t.Errorf("без strategy: %v", err)
	}
	c.Strategies[0].Variant = "bcn"
	if err := check(t, c); err != nil {
		t.Errorf("с strategy: %v", err)
	}
	c.Strategies[0].Variant = "satellite"
	if err := check(t, c); err == nil {
		t.Error("неизвестный вариант должен быть ошибкой")
	}
}

func TestValidateRejectsDuplicateKeyAndConflictingSharedParams(t *testing.T) {
	existing := valid()
	reg, err := domain.NewRegistry([]domain.Characteristic{existing})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateOne(reg, valid(), compute.Specs); err == nil || !strings.Contains(err.Error(), "уже есть") {
		t.Errorf("дубль ключа: %v", err)
	}

	walk := func(key string, speed float64) domain.Characteristic {
		return domain.Characteristic{
			Key: key, Title: "t", Description: "d", Level: domain.LevelHotel, ValueType: domain.ValueNumber,
			ValueField: "minutes", SignificantDiff: 5, TTLDays: 10,
			Strategies: []domain.Strategy{{Tool: "walk", Params: map[string]float64{"speed_m_per_min": speed},
				BaseReliability: 0.7, IndependenceGroup: "g"}},
		}
	}
	reg, _ = domain.NewRegistry([]domain.Characteristic{walk("walk_to_beach", 80), walk("walk_to_metro", 60)})
	if err := Validate(reg, compute.Specs); err == nil || !strings.Contains(err.Error(), "speed_m_per_min=80") {
		t.Errorf("разные значения общего параметра: %v", err)
	}
}

func TestValidateReportsAllErrorsAtOnce(t *testing.T) {
	c := valid()
	c.TTLDays, c.Level, c.Strategies[0].Tool = 0, "room", "magic-eye"
	err := check(t, c)
	if err == nil || strings.Count(err.Error(), "\n") < 2 {
		t.Errorf("ожидалось несколько ошибок разом: %v", err)
	}
}

func TestValidateOrigin(t *testing.T) {
	c := valid()
	c.Origin, c.OriginText, c.AddedAt = domain.OriginText, "есть ли рядом аптека", "2026-10-26"
	if err := check(t, c); err != nil {
		t.Errorf("корректный origin: %v", err)
	}
	for name, tt := range map[string]struct {
		mutate func(*domain.Characteristic)
		want   string
	}{
		"нет фразы":          {func(c *domain.Characteristic) { c.OriginText = "" }, "origin_text"},
		"плохая дата":        {func(c *domain.Characteristic) { c.AddedAt = "26.10.2026" }, "YYYY-MM-DD"},
		"неизвестный origin": {func(c *domain.Characteristic) { c.Origin = "llm" }, "допустимо text"},
		"фраза без origin":   {func(c *domain.Characteristic) { c.Origin = "" }, "только вместе с origin"},
	} {
		x := c
		tt.mutate(&x)
		if err := check(t, x); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// category: переносит характеристику уровня фасада на категории номеров.
func TestValidateCategoryRule(t *testing.T) {
	good := func() domain.Characteristic {
		return domain.Characteristic{
			Key: "sea_view", Title: "Вид", Description: "d", Level: domain.LevelFacade, ValueType: domain.ValueRange, ValueField: "min_floor",
			TTLDays: 365,
			Category: &domain.CategoryRule{
				Values: []string{"full", "none"}, ValueField: "view", Target: "full", FloorField: "min_floor", FavorableValues: []string{"full"},
			},
			Strategies: []domain.Strategy{{Tool: "sea-view", BaseReliability: 0.85, IndependenceGroup: "buildings_raycast"}},
		}
	}
	if err := check(t, good()); err != nil {
		t.Fatalf("корректное правило: %v", err)
	}
	for name, tt := range map[string]struct {
		mutate func(*domain.Characteristic)
		want   string
	}{
		"не фасад":            {func(c *domain.Characteristic) { c.Level = domain.LevelHotel }, "только характеристика уровня facade"},
		"цели нет в values":   {func(c *domain.Characteristic) { c.Category.Target = "maybe" }, `target "maybe"`},
		"одно значение":       {func(c *domain.Characteristic) { c.Category.Values = []string{"full"} }, "не меньше двух values"},
		"нет поля значения":   {func(c *domain.Characteristic) { c.Category.ValueField = "" }, "пустой value_field"},
		"выгодное не из set":  {func(c *domain.Characteristic) { c.Category.FavorableValues = []string{"great"} }, "favorable_values"},
		"values без этажа":    {func(c *domain.Characteristic) { c.Category.FloorField = "" }, "values должны совпадать"},
		"шаги без порядка":    {func(c *domain.Characteristic) { c.Category.ConflictSteps = 2 }, "только при ordered"},
		"отрицательный theta": {func(c *domain.Characteristic) { c.Category.ConflictTheta = -1 }, "conflict_theta"},
	} {
		c := good()
		tt.mutate(&c)
		err := check(t, c)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: ошибка %v, ждали %q", name, err, tt.want)
		}
	}
}

package search

import "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"

// DemoQuery — структурный запрос, в который разбирается фраза гостя. Разбор текста — задача 6.2; здесь запросы записаны вручную.
type DemoQuery struct {
	Text  string
	Query domain.Query
}

// DemoQueries — пять запросов проверки поиска (отчёт задачи 6.1, команда collect search-demo).
var DemoQueries = []DemoQuery{
	{"тихий номер с видом на море", domain.Query{Wishes: []domain.Desire{
		{Key: "noise_night", Value: "quiet", Importance: domain.Must},
		{Key: "sea_view", Importance: domain.Must},
	}}},
	{"до пляжа ≤ 10 минут и с собакой", domain.Query{Wishes: []domain.Desire{
		{Key: "walk_to_beach", Op: domain.OpLe, Value: 10.0, Importance: domain.Must},
		{Key: "pets_allowed", Importance: domain.Must},
	}}},
	{"тихо ночью, затемняющие шторы", domain.Query{Wishes: []domain.Desire{
		{Key: "noise_night", Value: "quiet", Importance: domain.Must},
		{Key: "blackout_curtains", Importance: domain.Prefer},
	}}},
	{"вид на море, бюджет до 150 €", domain.Query{
		Wishes:  []domain.Desire{{Key: "sea_view", Importance: domain.Must}},
		Filters: domain.Filters{PriceMax: 150},
	}},
	{"рядом ночная аптека, до метро ≤ 5 минут", domain.Query{Wishes: []domain.Desire{
		{Key: "night_pharmacy", Importance: domain.Must},
		{Key: "walk_to_metro", Op: domain.OpLe, Value: 5.0, Importance: domain.Must},
	}}},
}

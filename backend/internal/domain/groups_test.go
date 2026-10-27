package domain

import "testing"

// hotel собирает отель с оценками желаний: key=status, при StatusChance вероятность 0,3.
func gh(id int64, score float64, evals ...WishEval) HotelResult {
	return HotelResult{HotelID: id, Name: "H", Best: Variant{Score: score, Evals: evals}}
}

func ev(key string, st Status, imp Importance) WishEval {
	e := WishEval{Key: key, Status: st, Importance: imp}
	if st == StatusChance {
		e.P = 0.3
	}
	return e
}

func ids(hs []HotelResult) []int64 {
	var out []int64
	for _, h := range hs {
		out = append(out, h.HotelID)
	}
	return out
}

func eq(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildGroupsFullEnough(t *testing.T) {
	var hs []HotelResult
	for i := int64(1); i <= 3; i++ {
		hs = append(hs, gh(i, 0.9, ev("a", StatusMatch, Prefer), ev("b", StatusNear, Prefer)))
	}
	hs = append(hs, gh(9, 0.5, ev("a", StatusMatch, Prefer), ev("b", StatusMismatch, Prefer)))
	g := BuildGroups(hs, 5)
	if len(g) != 1 || g[0].Kind != GroupFull || len(g[0].Hotels) != 3 {
		t.Fatalf("три отеля «всё сразу» — блоков компромисса нет: %+v", g)
	}
}

func TestBuildGroupsCompromiseBlocks(t *testing.T) {
	hs := []HotelResult{
		gh(1, 0.7, ev("quiet", StatusMatch, Prefer), ev("beach", StatusMismatch, Prefer)),
		gh(2, 0.6, ev("quiet", StatusMatch, Prefer), ev("beach", StatusMismatch, Prefer)),
		gh(3, 0.65, ev("quiet", StatusMismatch, Prefer), ev("beach", StatusMatch, Prefer)),
		gh(4, 0.1, ev("quiet", StatusMismatch, Prefer), ev("beach", StatusMismatch, Prefer)), // две невыполненных: не показываем
		gh(5, 0.5, ev("quiet", StatusMatch, Prefer), ev("beach", StatusChance, Prefer)),      // шанс < 0,5 — не выполнено
	}
	g := BuildGroups(hs, 5)
	if len(g) != 2 || g[0].Kind != GroupCompromise || g[1].Kind != GroupCompromise {
		t.Fatalf("ждали два блока: %+v", g)
	}
	// Порядок блоков — по лучшей оценке: блок «без пляжа» (0,7) выше блока «без тишины» (0,65).
	if g[0].FailedKey != "beach" || !eq(ids(g[0].Hotels), []int64{1, 2, 5}) {
		t.Errorf("блок без пляжа: %v %v", g[0].FailedKey, ids(g[0].Hotels))
	}
	if g[1].FailedKey != "quiet" || !eq(ids(g[1].Hotels), []int64{3}) {
		t.Errorf("блок без тишины: %v %v", g[1].FailedKey, ids(g[1].Hotels))
	}
	for _, grp := range g {
		for _, h := range grp.Hotels {
			if h.HotelID == 4 {
				t.Error("отель с двумя невыполненными желаниями показан")
			}
		}
	}
}

func TestBuildGroupsKeepsFullAndAddsBlocks(t *testing.T) {
	hs := []HotelResult{
		gh(1, 0.9, ev("a", StatusMatch, Prefer), ev("b", StatusMatch, Prefer)),
		gh(2, 0.5, ev("a", StatusMatch, Prefer), ev("b", StatusMismatch, Prefer)),
	}
	g := BuildGroups(hs, 5)
	if len(g) != 2 || g[0].Kind != GroupFull || g[1].FailedKey != "b" {
		t.Fatalf("один «всё сразу» и блок: %+v", g)
	}
}

// Обязательное желание жертвуется, только если иначе выдача пуста; блок помечен.
func TestBuildGroupsMustSacrificedOnlyIfEmpty(t *testing.T) {
	hs := []HotelResult{
		gh(1, 0.4, ev("quiet", StatusMismatch, Must), ev("beach", StatusMatch, Prefer)),
		gh(2, 0.3, ev("quiet", StatusMatch, Must), ev("beach", StatusMismatch, Prefer)),
	}
	g := BuildGroups(hs, 5)
	if len(g) != 1 || g[0].FailedKey != "beach" || g[0].MustViolated || !eq(ids(g[0].Hotels), []int64{2}) {
		t.Fatalf("есть блок без пожелания — must не жертвуем: %+v", g)
	}
	only := []HotelResult{gh(1, 0.4, ev("quiet", StatusMismatch, Must), ev("beach", StatusMatch, Prefer))}
	g = BuildGroups(only, 5)
	if len(g) != 1 || !g[0].MustViolated || g[0].FailedKey != "quiet" {
		t.Fatalf("иначе пусто — блок с must и пометкой: %+v", g)
	}
}

func TestBuildGroupsRankedFallback(t *testing.T) {
	hs := []HotelResult{gh(1, 0.1, ev("a", StatusMismatch, Prefer), ev("b", StatusMismatch, Prefer))}
	g := BuildGroups(hs, 5)
	if len(g) != 1 || g[0].Kind != GroupRanked || len(g[0].Hotels) != 1 {
		t.Fatalf("блоков нет — плоская выдача: %+v", g)
	}
}

func TestGroupTitleAndLead(t *testing.T) {
	reg := Registry{}
	g := Group{Kind: GroupCompromise, FailedKey: "walk_to_beach", Hotels: []HotelResult{
		gh(1, 0.7, ev("noise_night", StatusMatch, Prefer), WishEval{Key: "walk_to_beach", Status: StatusMismatch, Value: 18.0}),
		gh(2, 0.6, ev("noise_night", StatusMatch, Prefer), WishEval{Key: "walk_to_beach", Status: StatusMismatch, Value: 44.0}),
	}}
	if got := GroupTitle(reg, g); got != "noise_night, но walk_to_beach — не совпало" && got != "noise_night, но walk_to_beach от 18" {
		t.Logf("заголовок без реестра: %q", got)
	}
	if GroupLead(reg, []Desire{{Key: "a"}, {Key: "b"}}, []Group{g}) == "" {
		t.Error("без полного совпадения нужна строка над блоками")
	}
	if GroupLead(reg, nil, []Group{{Kind: GroupFull}}) != "" {
		t.Error("без блоков компромисса строки нет")
	}
}

// «Под вопросом» и «данных нет» желание не нарушают, но и не подтверждают: такие отели не попадают в full.
func TestBuildGroupsOpenHotelsAreNotFull(t *testing.T) {
	hs := []HotelResult{
		gh(1, 0.9, ev("a", StatusMatch, Prefer), ev("b", StatusMatch, Prefer)),
		gh(2, 0.6, ev("a", StatusMatch, Prefer), ev("b", StatusUncertain, Prefer)),
		gh(3, 0.5, ev("a", StatusMatch, Prefer), ev("b", StatusUnknown, Prefer)),
	}
	g := BuildGroups(hs, 5)
	if len(g) != 2 || g[0].Kind != GroupFull || !eq(ids(g[0].Hotels), []int64{1}) || g[1].Kind != GroupOpen || !eq(ids(g[1].Hotels), []int64{2, 3}) {
		t.Fatalf("full=1, open=2: %+v", g)
	}
}

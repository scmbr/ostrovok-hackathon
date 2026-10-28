package lookup

import (
	"context"
	"strings"
	"testing"
)

type fakeStore struct{ hotels []Hotel }

func (f fakeStore) HotelByID(_ context.Context, _, id int64) (Hotel, bool, error) {
	for _, h := range f.hotels {
		if h.ID == id {
			return h, true, nil
		}
	}
	return Hotel{}, false, nil
}

// Как SQL: хотя бы одно слово встречается в названии или районе.
func (f fakeStore) HotelsLike(_ context.Context, _ int64, tokens []string) ([]Hotel, error) {
	var out []Hotel
	for _, h := range f.hotels {
		text := Normalize(h.Name + " " + h.District)
		for _, t := range tokens {
			if strings.Contains(text, t) {
				out = append(out, h)
				break
			}
		}
	}
	return out, nil
}

var hotels = []Hotel{
	{ID: 1, Name: "Hotel Arts Barcelona", District: "Vila Olímpica"},
	{ID: 2, Name: "Barceló Antenea Mar", District: "Poblenou"},
	{ID: 3, Name: "Hotel Attica 21", District: "Eixample"},
	{ID: 4, Name: "Gothic Cathedral Apartments", District: "Gothic"},
	{ID: 21, Name: "Hotel Sea Breeze", District: "Eixample"},
	{ID: 156, Name: "Serras Hotel Barcelona", District: "Gothic"},
	{ID: 326, Name: "Hotel Oasis", District: "Barceloneta"},
	{ID: 327, Name: "Hotel Mar Bella", District: "Poblenou"},
	{ID: 328, Name: "Hotel Gracia Suites", District: "Gracia"},
	{ID: 329, Name: "Gracia City Suites", District: "Gracia"},
}

func find(t *testing.T, q string) Result {
	t.Helper()
	got, err := (&Service{Store: fakeStore{hotels}, RegionID: 1}).Lookup(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// shown — совпадения, которые увидит гость (id или оценка не ниже порога).
func shown(r Result) []int64 {
	var ids []int64
	for _, m := range r.Matches {
		if m.By == MatchID || m.Score >= ShowThreshold {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// Таблица фраз: что видит гость (id отелей в порядке выдачи), и нет ли «отеля, которого нет в базе».
func TestLookupPhrases(t *testing.T) {
	cases := []struct {
		q       string
		want    []int64
		unknown string
	}{
		// Название внутри длинной фразы: ровно одна карточка.
		{"я хочу в serras hotel barcelona заселиться, у вас на сайте он представлен?", []int64{156}, ""},
		{"serras", []int64{156}, ""},
		{"Serras Hotel Barcelona", []int64{156}, ""},
		{"Hotel Arts Barcelona", []int64{1}, ""},
		{"barcelo antenea", []int64{2}, ""}, // без диакритики
		{"Attica 21", []int64{3}, ""},
		// Номер отеля.
		{"326", []int64{326}, ""},
		{" 21 ", []int64{21}, ""},
		{"999", nil, ""},
		// Желания и общие слова: ниже порога, решает агент.
		{"тихо ночью у моря", nil, ""},
		{"можно с собакой", nil, ""},
		{"quiet hotel near the sea", nil, ""},
		{"hotel", nil, ""},
		{"Hotel Barcelona", nil, ""},
		{"eixample", nil, ""},
		{"", nil, ""},
		// Одно слово из трёх в названии — слабое совпадение (1/3).
		{"gothic", nil, ""},
		// Назван отель, которого нет в базе.
		{"хочу заселиться в hotel foobar", nil, "foobar"},
		{"отель Zzyzx Palace есть?", nil, "zzyzx"},
		// Нет слова «отель» рядом с названием или это желания по-английски: не «нет в базе».
		{"foobar", nil, ""},
		{"hotel with sea view", nil, ""},
		{"отель с видом на sagrada familia", nil, ""},
	}
	for _, c := range cases {
		r := find(t, c.q)
		got := shown(r)
		if len(got) != len(c.want) || r.Unknown != c.unknown {
			t.Errorf("%q: показано %v (ждали %v), неизвестный %q (ждали %q); совпадения %+v", c.q, got, c.want, r.Unknown, c.unknown, r.Matches)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: показано %v, ждали %v", c.q, got, c.want)
				break
			}
		}
		t.Logf("%-75q → показано %v, неизвестный %q", c.q, got, r.Unknown)
	}
}

// Явный лидер остаётся один; при близких оценках показываются все.
func TestLookupLeaderOnly(t *testing.T) {
	// «gracia suites»: Hotel Gracia Suites (2 из 2) заметно впереди Gracia City Suites (2 из 3).
	r := find(t, "gracia suites")
	if len(r.Matches) != 1 || r.Matches[0].ID != 328 {
		t.Errorf("ждали одного лидера 328: %+v", r.Matches)
	}
	// «gracia»: оценки равны (по 0,5) — лидера нет, остаются оба.
	r = find(t, "gracia")
	if len(r.Matches) != 2 {
		t.Errorf("ждали двух равных совпадений: %+v", r.Matches)
	}
}

func TestLookupByIDAndTextTogether(t *testing.T) {
	// «21» — и id отеля, и слово названия; значимого слова нет, текстовых совпадений нет.
	r := find(t, "21")
	if len(r.Matches) != 1 || r.Matches[0].ID != 21 || r.Matches[0].By != MatchID || r.Matches[0].Score != 1 {
		t.Fatalf("id 21: %+v", r.Matches)
	}
}

func TestLookupLimit(t *testing.T) {
	var many []Hotel
	for i := int64(100); i < 120; i++ {
		many = append(many, Hotel{ID: i, Name: "Hotel Gracia Suites"})
	}
	got, _ := (&Service{Store: fakeStore{many}}).Lookup(context.Background(), "gracia suites")
	if len(got.Matches) != MaxResults {
		t.Errorf("результатов %d, ждали %d", len(got.Matches), MaxResults)
	}
}

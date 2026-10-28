// Package lookup — поиск отеля по названию или id: гость мог написать, какой отель ищет, а не какие у него желания.
package lookup

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

const (
	// MaxResults — сколько совпадений отдаёт поиск.
	MaxResults = 5
	// MaxQueryLen — длиннее запрос не похож на название отеля.
	MaxQueryLen = 200
	// maxTokens ограничивает число шаблонов ILIKE в запросе к БД.
	maxTokens = 8

	// ShowThreshold — с какой оценки совпадение по названию стоит показать гостю (id — всегда).
	ShowThreshold = 0.5
	// LeaderGap — на сколько первое совпадение должно опережать второе, чтобы остаться единственным.
	LeaderGap = 0.2
	// minWordLen — длина слова запроса, по которому совпадение считается значимым («mar», «de» — не имя).
	minWordLen = 4

	MatchID   = "id"
	MatchText = "text"
)

// Hotel — отель из хранилища: то, что нужно показать гостю.
type Hotel struct {
	ID       int64
	Name     string
	District string
	Card     domain.Card
}

// Store — порт хранилища. Адреса в данных нет, поэтому «адресом» служит район.
type Store interface {
	HotelByID(ctx context.Context, regionID, id int64) (Hotel, bool, error)
	// HotelsLike возвращает демо-отели региона, у которых название или район содержат хотя бы один из шаблонов
	// (слова без регистра и диакритики). Порядок не задан: оценку и порядок задаёт сервис.
	HotelsLike(ctx context.Context, regionID int64, tokens []string) ([]Hotel, error)
}

// Match — найденный отель с оценкой совпадения 0..1 и способом (id или текст).
type Match struct {
	Hotel
	Score float64
	By    string
}

type Service struct {
	Store    Store
	RegionID int64
}

// Result — найденные отели и, если гость назвал отель, которого нет в базе, это название.
type Result struct {
	Matches []Match
	// Unknown — слово из запроса, которое выглядит как название отеля (рядом с «hotel» или «отель»), но не встречается
	// ни в одном названии; Matches при этом пусты или слабы.
	Unknown string
}

// Lookup ищет отели по id (число) и по словам названия. Оценка — доля слов названия, названных в запросе;
// до MaxResults совпадений по убыванию оценки. Если первый заметно (на LeaderGap) впереди второго, остаётся он один.
func (s *Service) Lookup(ctx context.Context, q string) (Result, error) {
	q = strings.TrimSpace(q)
	res := Result{Matches: []Match{}}
	seen := map[int64]bool{}
	if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
		h, ok, err := s.Store.HotelByID(ctx, s.RegionID, id)
		if err != nil {
			return res, err
		}
		if ok {
			res.Matches = append(res.Matches, Match{Hotel: h, Score: 1, By: MatchID})
			seen[h.ID] = true
		}
	}
	qt := queryTokens(q)
	if len(qt) == 0 {
		return res, nil
	}
	if len(qt) > maxTokens {
		qt = qt[:maxTokens]
	}
	hotels, err := s.Store.HotelsLike(ctx, s.RegionID, qt)
	if err != nil {
		return res, err
	}
	type ranked struct {
		Match
		cover float64
	}
	var found []ranked
	for _, h := range hotels {
		if seen[h.ID] {
			continue
		}
		if sc := Score(qt, h.Name); sc > 0 {
			found = append(found, ranked{Match{Hotel: h, Score: sc, By: MatchText}, cover(qt, h.Name, h.District)})
		}
	}
	// Доля слов запроса нужна только для порядка при равной оценке.
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.cover != b.cover {
			return a.cover > b.cover
		}
		return a.Name < b.Name
	})
	for _, f := range found {
		res.Matches = append(res.Matches, f.Match)
	}
	if n := res.Matches; len(n) >= 2 && n[0].Score-n[1].Score >= LeaderGap {
		res.Matches = n[:1]
	}
	if len(res.Matches) > MaxResults {
		res.Matches = res.Matches[:MaxResults]
	}
	if len(res.Matches) == 0 || res.Matches[0].Score < ShowThreshold {
		res.Unknown = unknownName(q, qt, hotels)
	}
	return res, nil
}

// stop — слова, по которым отель не узнать: тип объекта, город, артикли.
var stop = map[string]bool{
	"hotel": true, "hotels": true, "hostal": true, "hostel": true, "the": true, "and": true,
	"de": true, "del": true, "la": true, "el": true, "les": true, "barcelona": true, "bcn": true,
	"отель": true, "отели": true, "гостиница": true, "барселона": true,
}

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ñ", "n", "ç", "c", "ё", "е",
)

// Normalize приводит текст к нижнему регистру без диакритики; те же замены делает SQL в хранилище.
func Normalize(s string) string { return accents.Replace(strings.ToLower(s)) }

func words(s string) []string {
	return strings.FieldsFunc(Normalize(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func significant(s string) []string {
	var out []string
	for _, w := range words(s) {
		if !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

// queryTokens — слова запроса, по которым ищем: без служебных, не короче трёх букв (числа — любые).
func queryTokens(q string) []string {
	var out []string
	for _, w := range significant(q) {
		if len([]rune(w)) >= 3 || strings.IndexFunc(w, unicode.IsDigit) >= 0 {
			out = append(out, w)
		}
	}
	return out
}

func hasPrefix(words []string, tok string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, tok) {
			return true
		}
	}
	return false
}

func runeLen(s string) int { return len([]rune(s)) }

// Score — оценка совпадения запроса с отелем, 0..1: доля слов названия (без служебных), названных в запросе.
// Нужно хотя бы одно значимое слово (не короче четырёх букв): длинная фраза с названием внутри проходит,
// набор общих слов («тихо ночью у моря») — нет.
func Score(queryTokens []string, name string) float64 {
	nameWords := significant(name)
	if len(nameWords) == 0 {
		return 0
	}
	covered, strong := 0, false
	for _, w := range nameWords {
		for _, t := range queryTokens {
			if strings.HasPrefix(w, t) {
				covered++
				strong = strong || runeLen(t) >= minWordLen
				break
			}
		}
	}
	if !strong {
		return 0
	}
	return math.Round(float64(covered)/float64(len(nameWords))*100) / 100
}

// cover — доля слов запроса, найденных в названии или районе; только для порядка при равной оценке.
func cover(queryTokens []string, name, district string) float64 {
	nameWords, districtWords := significant(name), words(district)
	matched := 0
	for _, t := range queryTokens {
		if hasPrefix(nameWords, t) || hasPrefix(districtWords, t) {
			matched++
		}
	}
	return float64(matched) / float64(len(queryTokens))
}

// typeWord — слово «отель» в запросе: после него (или перед ним) гость называет отель.
func typeWord(w string) bool {
	switch w {
	case "hotel", "hotels", "hostal", "hostel", "гостиница":
		return true
	}
	return strings.HasPrefix(w, "отел") || strings.HasPrefix(w, "гостиниц")
}

// wishWords — английские слова желаний: рядом с «hotel» они не название («hotel with sea view»).
var wishWords = map[string]bool{
	"with": true, "near": true, "view": true, "sea": true, "beach": true, "quiet": true, "cheap": true, "best": true,
	"good": true, "for": true, "from": true, "room": true, "rooms": true, "close": true, "nice": true, "cozy": true,
	"metro": true, "center": true, "centre": true, "family": true, "pets": true, "dog": true, "breakfast": true,
}

func isLatin(w string) bool {
	for _, r := range w {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return w != ""
}

// unknownName возвращает слово латиницей рядом со словом «отель», которого нет ни в одном названии отелей:
// гость назвал отель, которого нет в базе. hotels — отели, найденные по словам запроса.
func unknownName(q string, qt []string, hotels []Hotel) string {
	ws := words(q)
	inQuery := map[string]bool{}
	for _, t := range qt {
		inQuery[t] = true
	}
	for i, w := range ws {
		if !typeWord(w) {
			continue
		}
		for _, j := range []int{i - 1, i + 1} {
			if j < 0 || j >= len(ws) {
				continue
			}
			t := ws[j]
			if !inQuery[t] || !isLatin(t) || wishWords[t] || runeLen(t) < minWordLen {
				continue
			}
			known := false
			for _, h := range hotels {
				known = known || hasPrefix(significant(h.Name), t) || hasPrefix(words(h.District), t)
			}
			if !known {
				return t
			}
		}
	}
	return ""
}

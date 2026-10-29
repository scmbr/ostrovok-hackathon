package domain

import (
	"sort"
	"strings"
)

// SynonymOverlap — фразы двух разных характеристик, которые парсер без LLM мог бы принять друг за друга.
type SynonymOverlap struct {
	KeyA, PhraseA string
	KeyB, PhraseB string
}

// phraseEntry — фраза характеристики со словами в нормальной форме.
type phraseEntry struct {
	key, phrase string
	words       []string
}

func synonymEntries(reg Registry) []phraseEntry {
	var out []phraseEntry
	for _, c := range reg.All() {
		add := func(p string) {
			if w := PhraseWords(p); len(w) > 0 {
				out = append(out, phraseEntry{key: c.Key, phrase: strings.Join(w, " "), words: w})
			}
		}
		for _, p := range c.Synonyms {
			add(p)
		}
		for _, v := range sortedStringKeys(c.ValueSynonyms) {
			for _, p := range c.ValueSynonyms[v] {
				add(p)
			}
		}
	}
	return out
}

// wordsOverlap: слово синонима одной фразы подходит слову другой по правилу парсера (WordMatches: короткое — целиком,
// длинное — по первым пяти буквам) в любую сторону.
func wordsOverlap(a, b string) bool { return WordMatches(a, b) || WordMatches(b, a) }

// containsPhrase: фраза short целиком (подряд идущими словами) совпадает с частью фразы long.
func containsPhrase(long, short []string) bool {
	if len(short) == 0 || len(short) > len(long) {
		return false
	}
	for i := 0; i+len(short) <= len(long); i++ {
		ok := true
		for j := range short {
			if !wordsOverlap(long[i+j], short[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// SynonymOverlaps перечисляет пары фраз разных характеристик (synonyms и value_synonyms), где одна целиком входит в
// другую с учётом правила «первые пять букв». Такие фразы парсер сопоставляет по длине, и общая короткая фраза («ночной»)
// перехватывает слова фразы другой характеристики («ночной шум»). Неоднозначные фразы (ambiguous_synonyms) сюда не входят:
// они объявлены общими явно.
func SynonymOverlaps(reg Registry) []SynonymOverlap {
	es := synonymEntries(reg)
	var out []SynonymOverlap
	for i := range es {
		for j := range es {
			a, b := es[i], es[j]
			if i >= j || a.key == b.key {
				continue
			}
			if containsPhrase(a.words, b.words) || containsPhrase(b.words, a.words) {
				out = append(out, SynonymOverlap{a.key, a.phrase, b.key, b.phrase})
			}
		}
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].KeyA != out[y].KeyA {
			return out[x].KeyA < out[y].KeyA
		}
		return out[x].PhraseA < out[y].PhraseA
	})
	return out
}

// Происхождение synonyms у характеристики, добавленной текстом (Characteristic.SynonymsOrigin).
const (
	SynonymsDerived = "derived"
	SynonymsEdited  = "edited"
)

// DeriveSynonyms предлагает фразы гостя для характеристики, добавленной текстом, детерминированно и без модели: фраза из
// значимых слов названия и каждое значимое слово названия и исходной фразы (длиннее трёх букв, не служебное). Та же
// нормализация, что у парсера (PhraseWords). Пересечения с другими характеристиками здесь не проверяются: это делает
// SynonymOverlaps при сохранении.
func DeriveSynonyms(title, originText string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	significant := func(text string) []string {
		var ws []string
		for _, w := range PhraseWords(text) {
			if !stopWords[w] && len([]rune(w)) > 3 {
				ws = append(ws, w)
			}
		}
		return ws
	}
	tw := significant(title)
	if len(tw) > 1 {
		add(strings.Join(tw, " "))
	}
	for _, w := range tw {
		add(w)
	}
	for _, w := range significant(originText) {
		add(w)
	}
	return out
}

// OverlapsWith возвращает пересечения фразы с synonyms и value_synonyms других характеристик реестра reg
// и с фразами словаря типов заведений (poi_kinds.yaml)
// (по правилу SynonymOverlaps); ключ key исключается, чтобы характеристика не спорила сама с собой.
func OverlapsWith(reg Registry, key, phrase string) []SynonymOverlap {
	words := PhraseWords(phrase)
	var out []SynonymOverlap
	for _, e := range append(synonymEntries(reg), poiEntries(reg)...) {
		if e.key == key {
			continue
		}
		if containsPhrase(e.words, words) || containsPhrase(words, e.words) {
			out = append(out, SynonymOverlap{KeyA: key, PhraseA: strings.Join(words, " "), KeyB: e.key, PhraseB: e.phrase})
		}
	}
	return out
}

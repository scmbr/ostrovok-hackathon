package domain

import (
	"strconv"
	"strings"
)

// Команда сравнения в чате: «сравни первый и третий», «сравни Hotel Arts и Hotel Omm», «сравни шортлист».
// Распознаётся кодом до вызова модели, поэтому поведение диалога с моделью (и его кеш) не меняется.

// CompareCommand — разобранная команда.
type CompareCommand struct {
	// Shortlist — сравнить отели шортлиста.
	Shortlist bool
	// Refs — ссылки на отели выдачи: номер или название.
	Refs []SimilarRef
}

// OrdinalOf возвращает номер по слову-порядковому («первый» → 1); 0 — не порядковое.
func OrdinalOf(w string) int { return ordinalOf(w) }

var compareFiller = map[string]bool{"отель": true, "отеля": true, "отелем": true, "отеле": true, "вариант": true, "номер": true,
	"пожалуйста": true, "мне": true, "их": true}

// ParseCompareCommand разбирает команду сравнения. ok == false: фраза не начинается со слова «сравни…».
func ParseCompareCommand(text string) (CompareCommand, bool) {
	words := PhraseWords(text)
	if len(words) == 0 || !strings.HasPrefix(words[0], "сравн") {
		return CompareCommand{}, false
	}
	var cmd CompareCommand
	var part []string
	flush := func() {
		if len(part) == 0 {
			return
		}
		defer func() { part = nil }()
		if len(part) == 1 {
			if n, err := strconv.Atoi(part[0]); err == nil && n > 0 {
				cmd.Refs = append(cmd.Refs, SimilarRef{Ordinal: n})
				return
			}
			if n := OrdinalOf(part[0]); n > 0 {
				cmd.Refs = append(cmd.Refs, SimilarRef{Ordinal: n})
				return
			}
		}
		cmd.Refs = append(cmd.Refs, SimilarRef{Name: strings.Join(part, " ")})
	}
	for _, w := range words[1:] {
		switch {
		case strings.HasPrefix(w, "шортлист") || strings.HasPrefix(w, "избранн"):
			cmd.Shortlist = true
		case w == "и" || w == "с" || w == "со" || w == "vs" || w == "против":
			flush()
		case compareFiller[w]:
			// служебное слово
		default:
			part = append(part, w)
		}
	}
	flush()
	if cmd.Shortlist {
		cmd.Refs = nil
	}
	return cmd, true
}

package domain

import (
	"strings"
	"unicode"
)

// PhraseWords приводит фразу к словам для сопоставления: нижний регистр, «ё» как «е», всё кроме букв и цифр разделяет слова.
func PhraseWords(s string) []string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// stemLen — длина основы: синоним длиннее этого сопоставляется по началу слова, короче или равный — только целым словом.
const stemLen = 5

// WordMatches сообщает, подходит ли слово гостя к слову синонима. Короткий синоним (до 5 букв) — только целое слово,
// иначе «метро» совпало бы с «метров», а «море» — с «морской»; формы короткого слова перечисляются в реестре явно.
// Длинный синоним сопоставляется по первым пяти буквам: «собака» — «собакой», «питомец» — «питомцем».
func WordMatches(synonym, word string) bool {
	rs := []rune(synonym)
	if len(rs) <= stemLen {
		return synonym == word
	}
	return strings.HasPrefix(word, string(rs[:stemLen]))
}

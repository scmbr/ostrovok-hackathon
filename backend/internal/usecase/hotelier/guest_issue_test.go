package hotelier

import (
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Вопрос по конфликту «гости против расчёта» остаётся подтверждением расчёта («верно?»), но называет гостей и
// возможные причины: деревья, леса, стройка.
func TestCategoryDraftMentionsGuests(t *testing.T) {
	c := domain.Characteristic{Key: domain.SeaViewKey, Title: "Вид на море", Category: &domain.CategoryRule{Values: []string{"full", "none"}, Target: "full"}}
	is := CategoryIssue{Name: "Sea View", CalcValue: "full", GuestAgainst: 3, Placements: []IssuePlacement{{Facade: "фасад на юго-восток", From: 1, To: 3}}}
	got := categoryDraft(c, is)
	for _, want := range []string{"3 гостя сообщили", "деревья", "строительные леса", "стройка", "верно?", "море видно"} {
		if !strings.Contains(got, want) {
			t.Errorf("в вопросе нет %q: %s", want, got)
		}
	}
	is.GuestAgainst = 0
	if strings.Contains(categoryDraft(c, is), "гост") {
		t.Error("без гостей вопрос не должен их упоминать")
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{1: "1 гость", 2: "2 гостя", 5: "5 гостей", 11: "11 гостей", 21: "21 гость", 24: "24 гостя"} {
		if got := plural(n, "гость", "гостя", "гостей"); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}

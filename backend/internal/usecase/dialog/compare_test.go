package dialog

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/booking"
)

type fakeComparer struct{ got []booking.CompareRequest }

func (f *fakeComparer) Compare(_ context.Context, req booking.CompareRequest) (booking.Compared, error) {
	f.got = append(f.got, req)
	return booking.Compared{Comparison: domain.Comparison{Winner: 0, Verdict: "Для вас лучше A."}}, nil
}

func kinds(evs []Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

// «сравни первый и третий» берёт отели из последней выдачи и отдаёт событие compare без поиска и без модели.
func TestCompareCommandByOrdinal(t *testing.T) {
	r := newRig(t, seaOrBeach)
	cmp := &fakeComparer{}
	r.e.Compare = cmp
	turn(t, r, Request{SessionID: "s1", Message: "хочу вид на море"})
	searches := len(r.search.queries)
	evs := turn(t, r, Request{SessionID: "s1", Message: "сравни первый и третий"})
	if got := kinds(evs); !slices.Contains(got, EventCompare) || slices.Contains(got, EventResults) {
		t.Fatalf("события %v", got)
	}
	if len(cmp.got) != 1 || !slices.Equal(cmp.got[0].HotelIDs, []int64{1, 3}) {
		t.Errorf("сравнение: %+v", cmp.got)
	}
	if len(r.search.queries) != searches {
		t.Error("команда сравнения не должна запускать поиск")
	}
}

func TestCompareCommandByNameAndShortlist(t *testing.T) {
	r := newRig(t, seaOrBeach)
	cmp := &fakeComparer{}
	r.e.Compare = cmp
	turn(t, r, Request{SessionID: "s2", Message: "сравни Hotel Arts и Casa Fuster"})
	if len(cmp.got) != 1 || !slices.Equal(cmp.got[0].HotelIDs, []int64{1, 3}) {
		t.Fatalf("по названиям: %+v", cmp.got)
	}
	// шортлист из сессии (его пишет booking.Service в то же JSON-состояние)
	r.e.Sessions.(memSessions)["s3"] = []byte(`{"shortlist":[2,4,5,6]}`)
	evs := turn(t, r, Request{SessionID: "s3", Message: "сравни шортлист"})
	if len(cmp.got) != 2 || !slices.Equal(cmp.got[1].HotelIDs, []int64{2, 4, 5}) {
		t.Fatalf("шортлист: %+v", cmp.got)
	}
	var msg string
	for _, e := range evs {
		if m, ok := e.Data.(MessageEvent); ok {
			msg = m.Text
		}
	}
	if !strings.Contains(msg, "первые 3") {
		t.Errorf("о срезе шортлиста гостю сказано: %q", msg)
	}
	// пустой шортлист и непонятный отель — понятное сообщение, без сравнения
	turn(t, r, Request{SessionID: "s4", Message: "сравни шортлист"})
	turn(t, r, Request{SessionID: "s4", Message: "сравни первый и второй"})
	if len(cmp.got) != 2 {
		t.Errorf("сравнение не должно запускаться: %d", len(cmp.got))
	}
}

// Без Comparer и для обычных фраз команда не срабатывает: диалог идёт прежним путём.
func TestCompareCommandIgnoredWithoutComparer(t *testing.T) {
	r := newRig(t, seaOrBeach)
	evs := turn(t, r, Request{SessionID: "s5", Message: "сравни первый и третий"})
	if slices.Contains(kinds(evs), EventCompare) {
		t.Errorf("события %v", kinds(evs))
	}
}

// day_part нужен только сравнению: в list_characteristics агента диалога он не попадает, иначе кеш диалога устарел бы.
func TestListCharacteristicsHasNoDayPart(t *testing.T) {
	r := newRig(t, seaOrBeach)
	out, err := (&run{e: r.e}).toolList()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "day_part") || strings.Contains(out, "morning") {
		t.Errorf("list_characteristics выдаёт day_part: %.200s", out)
	}
}

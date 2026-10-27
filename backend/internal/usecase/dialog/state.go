// Пакет dialog — агент диалога с гостем: свободная фраза → структурный запрос из 6.1, уточнение только когда ответ
// меняет выдачу, объяснение выдачи без выдуманных фактов. Всё, что решает судьбу выдачи, — код; LLM (если есть)
// только пересказывает готовый текст и выбирает инструменты. Описание — docs/dialog.md.
package dialog

import (
	"slices"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// SessionTTL — сколько живёт сессия после последнего обращения.
const SessionTTL = time.Hour

// Лимиты уточнений (правило в коде, а не в подсказке модели).
const (
	MaxClarificationsPerMessage = 1
	MaxClarificationsPerSession = 2
)

// Shown — отель из последней выдачи: на него гость ссылается номером или названием.
type Shown struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Pending — вопрос, ответа на который ждём; пока он висит, желание в запрос не входит.
type Pending struct {
	Kind    string                  `json:"kind"` // KindAmbiguity | KindImportance
	Phrase  string                  `json:"phrase"`
	Options []domain.Interpretation `json:"options"`
}

const (
	KindAmbiguity  = "ambiguity"
	KindImportance = "importance"
)

// State — состояние сессии. Хранится в Postgres (dialog_sessions), профиль живёт только здесь, на уровне сессии.
type State struct {
	Wishes   []domain.Desire `json:"wishes"`
	Filters  domain.Filters  `json:"filters"`
	MinFloor int             `json:"min_floor,omitempty"`
	Last     []Shown         `json:"last,omitempty"`
	Current  int64           `json:"current,omitempty"`
	// Asked — сколько уточнений уже задано в сессии.
	Asked int `json:"asked,omitempty"`
	// Settled — желания, важность которых уже решена вопросом или допущением: не переспрашиваем.
	Settled []string `json:"settled,omitempty"`
	Pending *Pending `json:"pending,omitempty"`
}

func (s *State) wish(key string) (domain.Desire, bool) {
	for _, w := range s.Wishes {
		if w.Key == key {
			return w, true
		}
	}
	return domain.Desire{}, false
}

// upsert заменяет желание с тем же ключом или добавляет новое.
func upsert(ws []domain.Desire, d domain.Desire) []domain.Desire {
	out := slices.Clone(ws)
	for i := range out {
		if out[i].Key == d.Key {
			out[i] = d
			return out
		}
	}
	return append(out, d)
}

func (s *State) settle(key string) {
	if !slices.Contains(s.Settled, key) {
		s.Settled = append(s.Settled, key)
	}
}

// withFloor подставляет этаж сессии в желание фасадной характеристики.
func (s *State) withFloor(reg domain.Registry, d domain.Desire) domain.Desire {
	if c, ok := reg.Get(d.Key); ok && domain.CategoryLevel(c) && d.MinFloor == 0 {
		d.MinFloor = s.MinFloor
	}
	return d
}

// Merge вносит разобранное сообщение в состояние: желания копятся, «без X» убирает, «заново» сбрасывает.
// Неоднозначные фразы сюда не входят — их решает уточнение.
func (s *State) Merge(reg domain.Registry, p domain.ParseResult) {
	if p.Reset {
		*s = State{Current: s.Current, Last: s.Last, Asked: s.Asked}
	}
	for _, key := range p.Remove {
		s.Wishes = slices.DeleteFunc(s.Wishes, func(w domain.Desire) bool { return w.Key == key })
	}
	if p.MinFloor > 0 {
		s.MinFloor = p.MinFloor
		for i, w := range s.Wishes {
			if c, ok := reg.Get(w.Key); ok && domain.CategoryLevel(c) {
				s.Wishes[i].MinFloor = p.MinFloor
			}
		}
	}
	for _, w := range p.Wishes {
		s.Wishes = upsert(s.Wishes, s.withFloor(reg, w))
	}
	if p.Filters.PriceMax > 0 {
		s.Filters.PriceMax = p.Filters.PriceMax
	}
	if p.Filters.StarsMin > 0 {
		s.Filters.StarsMin = p.Filters.StarsMin
	}
}

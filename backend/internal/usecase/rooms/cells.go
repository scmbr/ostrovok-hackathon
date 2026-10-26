package rooms

import (
	"context"
	"fmt"
	"sort"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// Cells — значения характеристик категорий отеля по расчёту в ячейках «фасад × этаж». Нужны, чтобы понять, к чему
// относится ответ гостя: гость с этажа, где расчёт даёт другое значение, чем у категории в целом, категорию не
// опровергает («с 2-го этажа моря не видно» не противоречит «видно с 4-го»).
type Cells struct {
	prs  map[string]projection
	cats map[int64]domain.RoomCategory
}

// Cells собирает ячейки отеля по характеристикам с правилом category.
func (s *Service) Cells(ctx context.Context, hotelID int64) (*Cells, error) {
	cats, err := s.Store.Categories(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	facts, err := s.Store.FacadeFacts(ctx, hotelID, projectionKeys(s.Registry))
	if err != nil {
		return nil, fmt.Errorf("факты фасадов: %w", err)
	}
	c := &Cells{prs: map[string]projection{}, cats: map[int64]domain.RoomCategory{}}
	for _, pr := range projections(s.Registry, facts) {
		c.prs[pr.char.Key] = pr
	}
	for _, cat := range cats {
		c.cats[cat.ID] = cat
	}
	return c, nil
}

// ValueAt — значение характеристики key у фасада на этаже по расчёту; false — значения нет.
func (c *Cells) ValueAt(key string, facadeID int64, floor int) (string, bool) {
	pr, ok := c.prs[key]
	if !ok {
		return "", false
	}
	return pr.valueAt(facadeID, floor)
}

// Dominant — значение характеристики key, преобладающее в категории по расчёту (то же, что в свидетельстве категории).
func (c *Cells) Dominant(key string, categoryID int64) (string, bool) {
	pr, ok := c.prs[key]
	cat, okCat := c.cats[categoryID]
	if !ok || !okCat {
		return "", false
	}
	v, share := dominant(pr.rule.Values, pr.assess(cat).Shares)
	return v, share > 0
}

// Category — категория отеля по id.
func (c *Cells) Category(id int64) (domain.RoomCategory, bool) {
	cat, ok := c.cats[id]
	return cat, ok
}

// Categories — категории отеля по возрастанию id.
func (c *Cells) Categories() []domain.RoomCategory {
	out := make([]domain.RoomCategory, 0, len(c.cats))
	for _, cat := range c.cats {
		out = append(out, cat)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Assess — оценка категории по характеристике key: вероятность, гарантия, пожелание.
func (c *Cells) Assess(key string, categoryID int64) (domain.CategoryAssessment, bool) {
	pr, ok := c.prs[key]
	cat, okCat := c.cats[categoryID]
	if !ok || !okCat {
		return domain.CategoryAssessment{}, false
	}
	return pr.assess(cat), true
}

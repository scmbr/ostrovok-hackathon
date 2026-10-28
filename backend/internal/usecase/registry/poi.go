package registry

import (
	"fmt"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

// checkPoi проверяет словарь типов заведений реестра (registry/poi_kinds.yaml): поля заполнены, формы заголовков различаются,
// фразы не пересекаются с фразами характеристик и друг с другом (характеристики реестра разбираются раньше, но фраза не должна
// зависеть от порядка).
func checkPoi(reg domain.Registry) []error {
	var errs []error
	for _, k := range reg.Poi().All() {
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("poi_kinds %s: %s", k.ID, fmt.Sprintf(format, args...)))
		}
		if !keyPattern.MatchString(k.ID) {
			bad("id должен быть в snake_case латиницей, получено %q", k.ID)
		}
		if len(k.Kinds) == 0 {
			bad("не заданы kinds (значения pois.kind)")
		}
		for name, v := range map[string]string{"label": k.Label, "title": k.Title, "title_met": k.TitleMet, "title_unmet": k.TitleUnmet, "icon": k.Icon, "color": k.Color} {
			if strings.TrimSpace(v) == "" {
				bad("пустое поле %s", name)
			}
		}
		if k.TitleMet != "" && k.TitleMet == k.TitleUnmet {
			bad("title_met и title_unmet совпадают")
		}
		for _, p := range k.Synonyms {
			if len(domain.PhraseWords(p)) == 0 {
				bad("пустой синоним %q", p)
			}
		}
		for _, kind := range k.Kinds {
			if _, ok := k.Labels[kind]; !ok && len(k.Kinds) > 1 {
				bad("у группы нет подписи labels для %s", kind)
			}
		}
	}
	for _, o := range domain.PoiOverlaps(reg) {
		errs = append(errs, fmt.Errorf("poi_kinds: фраза «%s» (%s) пересекается с фразой «%s» (%s)", o.PhraseA, o.KeyA, o.PhraseB, o.KeyB))
	}
	return errs
}

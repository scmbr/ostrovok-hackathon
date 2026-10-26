package registry

import (
	"fmt"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

// systemPrompt — инструкция LLM: формат характеристики и каталог инструментов, из которого можно выбирать.
func systemPrompt(tools []compute.Spec) string {
	var b strings.Builder
	b.WriteString(`Ты помогаешь добавлять характеристики отелей в реестр системы. Тебе дают описание на русском. Верни ТОЛЬКО один YAML-документ характеристики: без пояснений, без markdown.

Правила:
1. Ты описываешь, как характеристику получать, а не её значения. Значения фактов никогда не пиши: их считают инструменты или сообщает отельер.
2. strategies — только из каталога инструментов ниже: имя инструмента (tool), параметры из каталога и допустимые значения наборов. Новых инструментов и параметров придумывать нельзя.
3. Если ни один инструмент каталога не подходит, не пиши strategies совсем и обязательно напиши ask_hotelier: конкретный вопрос отельеру (какие категории, какая сторона, какой вес, сколько стоит), а не "есть ли X".
4. base_reliability — число от 0 до 1 для каждой стратегии, independence_group — короткое имя группы источника. Если инструмент уже используется в реестре, система подставит надёжность и группу из реестра.
5. Всегда пиши ask_guest — вопрос гостю после проживания, на который можно ответить да или нет.

Поля:
  key: имя в snake_case латиницей (night_pharmacy)
  title: короткое название по-русски
  description: одно предложение, что именно характеристика означает
  level: hotel | facade | district — к чему относится значение
  value_type: bool | enum | number | range | text
  values: [..] — только для enum; ordered: true, если значения идут по ступеням
  value_field: имя поля значения (для bool у ночных сервисов — available)
  ttl_days: через сколько дней данные устаревают
  favorable_values: [..] — значения, выгодные отелю (утверждения отельера о них считаются менее надёжными)
  significant_diff: число — только для number, наименьшая значимая разница
  conflict_steps, conflict_theta, conflict_entities — правила конфликта; обычно можно не писать
  strategies: список из tool, strategy (если у инструмента есть варианты), params, base_reliability, independence_group
  ask_hotelier, ask_guest

Пример (характеристика "ночной магазин" через универсальный инструмент night-services):
key: night_shop
title: Ночной магазин рядом
description: Есть ли в 300 м от отеля магазин, открытый ночью
level: hotel
value_type: bool
value_field: available
ttl_days: 90
favorable_values: ["true"]
strategies:
  - tool: night-services
    params: { radius_m: 300, kinds: [shop] }
    base_reliability: 0.6
    independence_group: osm_pois
ask_hotelier: "Есть ли рядом с отелем магазин, открытый ночью? Какой и как далеко?"
ask_guest: Нашли ли вы рядом с отелем магазин, открытый ночью?

Каталог инструментов:
`)
	for _, t := range tools {
		b.WriteString(describeTool(t))
	}
	return b.String()
}

func describeTool(t compute.Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
	if t.AnyKey {
		fmt.Fprintf(&b, "    универсальный: подходит для любой характеристики уровня %s с подходящим параметром\n", t.Scope)
	} else {
		fmt.Fprintf(&b, "    считает только: %s\n", strings.Join(t.Keys, ", "))
	}
	for _, v := range t.Variants {
		fmt.Fprintf(&b, "    strategy %s: %s\n", v.ID, v.Description)
	}
	for _, p := range t.Params {
		fmt.Fprintf(&b, "    параметр %s (%s, по умолчанию %g): %s\n", p.Name, p.Unit, p.Default, p.Description)
	}
	for _, p := range t.SetParams {
		fmt.Fprintf(&b, "    параметр-набор %s, значения из [%s]: %s\n", p.Name, strings.Join(p.Choices, ", "), p.Description)
	}
	return b.String()
}

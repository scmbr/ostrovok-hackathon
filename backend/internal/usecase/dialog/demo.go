package dialog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed demo_dialogs.json
var demoDialogsJSON []byte

// DemoDialog — демо-диалог, записанный в кеш (adapter/llm/cache/dialog/<Name>.json). Turns — реплики гостя по ходам;
// «@id» — ответ кнопкой на уточнение (как в `collect dialog`).
type DemoDialog struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Turns []string `json:"turns"`
}

// Example — пример первой фразы для кнопки в чате.
type Example struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// DemoDialogs возвращает демо-диалоги из встроенной фикстуры.
func DemoDialogs() ([]DemoDialog, error) {
	var ds []DemoDialog
	if err := json.Unmarshal(demoDialogsJSON, &ds); err != nil {
		return nil, fmt.Errorf("demo_dialogs.json: %w", err)
	}
	for _, d := range ds {
		if d.Name == "" || len(d.Turns) == 0 || strings.HasPrefix(d.Turns[0], "@") {
			return nil, fmt.Errorf("demo_dialogs.json: диалог %q должен начинаться с фразы гостя", d.Name)
		}
	}
	return ds, nil
}

// DemoDialogByName возвращает диалог по имени.
func DemoDialogByName(name string) (DemoDialog, bool) {
	ds, err := DemoDialogs()
	if err != nil {
		return DemoDialog{}, false
	}
	for _, d := range ds {
		if d.Name == name {
			return d, true
		}
	}
	return DemoDialog{}, false
}

// Examples — первые фразы демо-диалогов для кнопок-примеров.
func Examples() ([]Example, error) {
	ds, err := DemoDialogs()
	if err != nil {
		return nil, err
	}
	out := make([]Example, len(ds))
	for i, d := range ds {
		out[i] = Example{ID: d.Name, Title: d.Title, Text: d.Turns[0]}
	}
	return out, nil
}

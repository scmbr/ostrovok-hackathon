package labels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodMarks = `
    marks:
      sea_view.has_view: { value: true, confidence: sure, source: "https://example.org/a" }
      noise_night.quiet_rooms: { confidence: unknown, source: "https://example.org/b" }
      noise_night.street_noisy: { value: false, confidence: likely, source: "https://example.org/c" }
`

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadDirOK(t *testing.T) {
	dir := write(t, "labeled_at: \"2026-10-28\"\nhotels:\n  - id: 7\n    name: X\n    group: urban"+goodMarks)
	ls, err := LoadDir(dir)
	if err != nil || len(ls) != 1 || ls[0].HotelID != 7 || len(ls[0].Marks) != 3 {
		t.Fatalf("%+v, %v", ls, err)
	}
}

func TestLoadDirRejectsBadMarks(t *testing.T) {
	for name, body := range map[string]string{
		"нет value при sure": "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: urban\n    marks:\n      sea_view.has_view: { confidence: sure, source: s }\n      noise_night.quiet_rooms: { confidence: unknown, source: s }\n      noise_night.street_noisy: { confidence: unknown, source: s }\n",
		"value при unknown":  "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: urban\n    marks:\n      sea_view.has_view: { value: true, confidence: unknown, source: s }\n      noise_night.quiet_rooms: { confidence: unknown, source: s }\n      noise_night.street_noisy: { confidence: unknown, source: s }\n",
		"нет источника":      "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: urban\n    marks:\n      sea_view.has_view: { value: true, confidence: sure }\n      noise_night.quiet_rooms: { confidence: unknown, source: s }\n      noise_night.street_noisy: { confidence: unknown, source: s }\n",
		"нет цели":           "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: urban\n    marks:\n      sea_view.has_view: { value: true, confidence: sure, source: s }\n",
		"плохая группа":      "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: inland" + goodMarks,
		"опечатка в поле":    "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: urban\n    grup: x" + goodMarks,
	} {
		if _, err := LoadDir(write(t, body)); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
	dup := "labeled_at: \"x\"\nhotels:\n  - id: 7\n    group: urban" + goodMarks + "  - id: 7\n    group: urban" + goodMarks
	if _, err := LoadDir(write(t, dup)); err == nil || !strings.Contains(err.Error(), "7") {
		t.Errorf("дубль отеля: %v", err)
	}
}

// Файл разметки из репозитория проходит проверку.
func TestRepositoryLabelsAreValid(t *testing.T) {
	ls, err := LoadDir(filepath.Join("..", "..", "..", "..", "data", "labels"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) < 15 || len(ls) > 20 {
		t.Errorf("размечено %d отелей, нужно 15–20", len(ls))
	}
	need := map[int64]bool{326: false, 156: false, 161: false, 57: false}
	coastal, urban := 0, 0
	for _, l := range ls {
		if _, ok := need[l.HotelID]; ok {
			need[l.HotelID] = true
		}
		if l.Group == "coastal" {
			coastal++
		} else {
			urban++
		}
	}
	for id, ok := range need {
		if !ok {
			t.Errorf("обязательный отель %d не размечен", id)
		}
	}
	if coastal != urban {
		t.Errorf("береговых %d, городских %d: нужно поровну", coastal, urban)
	}
}

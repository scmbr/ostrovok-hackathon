package dialog

import "testing"

func TestDemoDialogsFixture(t *testing.T) {
	ds, err := DemoDialogs()
	if err != nil || len(ds) != 3 {
		t.Fatalf("%v %d", err, len(ds))
	}
	ex, _ := Examples()
	if len(ex) != 3 || ex[0].Text != ds[0].Turns[0] || ex[0].ID != "clarify-beach" {
		t.Errorf("примеры: %+v", ex)
	}
	if _, ok := DemoDialogByName("unmet-night"); !ok {
		t.Error("диалог по имени не найден")
	}
}

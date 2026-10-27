package domain

import (
	"reflect"
	"testing"
)

func synReg(t *testing.T, cs ...Characteristic) Registry {
	t.Helper()
	reg, err := NewRegistry(cs)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestDeriveSynonyms(t *testing.T) {
	got := DeriveSynonyms("Ночной магазин рядом", "есть ли рядом круглосуточный магазин")
	want := []string{"ночной магазин", "ночной", "магазин", "круглосуточный"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeriveSynonyms = %q, ожидалось %q", got, want)
	}
	// Короткие и служебные слова не берутся.
	if got := DeriveSynonyms("Есть ли лифт", "лифт"); !reflect.DeepEqual(got, []string{"лифт"}) {
		t.Errorf("короткие слова: %q", got)
	}
}

// Короткая общая фраза перехватила бы слова чужой: «ночной» входит в «ночной шум». Основа сравнивается по первым пяти
// буквам, как в парсере.
func TestSynonymOverlaps(t *testing.T) {
	reg := synReg(t,
		Characteristic{Key: "noise_night", Synonyms: []string{"ночной шум", "тишина"}},
		Characteristic{Key: "night_shop", Synonyms: []string{"ночной"}},
		Characteristic{Key: "pets", Synonyms: []string{"собака", "питомец"}},
	)
	os := SynonymOverlaps(reg)
	if len(os) != 1 || os[0].KeyA != "night_shop" && os[0].KeyB != "night_shop" {
		t.Fatalf("пересечения: %+v", os)
	}
	// «собаками» подходит к «собака» по основе из пяти букв, а «собак» короче пяти и требует целого слова.
	if got := OverlapsWith(reg, "new", "собаками"); len(got) != 1 || got[0].KeyB != "pets" {
		t.Errorf("основа из пяти букв: %+v", got)
	}
	if got := OverlapsWith(reg, "new", "лифт"); len(got) != 0 {
		t.Errorf("лифт: %+v", got)
	}
	// Своя же фраза не спорит сама с собой.
	if got := OverlapsWith(reg, "pets", "собака"); len(got) != 0 {
		t.Errorf("собственная фраза: %+v", got)
	}
}

package domain

import (
	"strings"
	"testing"
)

func petsTruth(avail bool) map[string]any {
	if !avail {
		return map[string]any{"available": false}
	}
	return map[string]any{"available": true, "species": []string{"dogs", "cats"}, "max_kg": 10, "fee_eur": 15}
}

func TestPersonaAnswers(t *testing.T) {
	r := AnswerRequest{HotelID: 7, Key: "pets_allowed", Format: FormatOpen, Truth: petsTruth(true)}
	if a := PersonaAnswer(PersonaHonest, r); !strings.Contains(a, "собак и кошек") || !strings.Contains(a, "10 кг") || !strings.Contains(a, "15 €") {
		t.Errorf("честный отвечает полно: %q", a)
	}
	r.Truth = petsTruth(false)
	if a := PersonaAnswer(PersonaHonest, r); !strings.HasPrefix(a, "Нет") {
		t.Errorf("честный признаёт недостаток: %q", a)
	}
	// Приукрашивающий говорит «да» независимо от истины.
	if a := PersonaAnswer(PersonaEmbellishing, r); !strings.HasPrefix(a, "Да") {
		t.Errorf("приукрашивающий: %q", a)
	}
	r.Key, r.Truth = "blackout_curtains", map[string]any{"available": false}
	if a := PersonaAnswer(PersonaEmbellishing, r); !strings.HasPrefix(a, "Да") {
		t.Errorf("приукрашивающий, шторы: %q", a)
	}
	// Подтверждение расчёта: приукрашивающий соглашается всегда.
	c := AnswerRequest{HotelID: 7, Key: "sea_view", Format: FormatConfirm}
	if a := PersonaAnswer(PersonaEmbellishing, c); !strings.HasPrefix(a, "Да") {
		t.Errorf("подтверждение: %q", a)
	}
}

func TestLazyIsShortAndSometimesEvasive(t *testing.T) {
	shy, plain := 0, 0
	for id := int64(1); id <= 300; id++ {
		r := AnswerRequest{HotelID: id, Key: "pets_allowed", Format: FormatOpen, Truth: petsTruth(true)}
		a := PersonaAnswer(PersonaLazy, r)
		if len([]rune(a)) > 40 {
			t.Fatalf("ленивый отвечает коротко: %q", a)
		}
		if a != PersonaAnswer(PersonaLazy, r) {
			t.Fatal("ответ не детерминирован")
		}
		if strings.Contains(a, "уточнить") {
			shy++
		} else {
			plain++
		}
	}
	if shy < 70 || shy > 130 || plain == 0 {
		t.Errorf("ленивый уходит от ответа примерно в трети случаев: %d из 300", shy)
	}
}

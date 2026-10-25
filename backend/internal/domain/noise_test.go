package domain

import (
	"math"
	"testing"
)

func TestNoiseLevel(t *testing.T) {
	cases := map[float64]string{30: NoiseQuiet, 45: NoiseQuiet, 45.1: NoiseModerate, 55: NoiseModerate, 55.1: NoiseLoud, 70: NoiseLoud}
	for ln, want := range cases {
		if got := NoiseLevel(ln); got != want {
			t.Errorf("Ln %v: %s, want %s", ln, got, want)
		}
	}
}

func TestComputeFacadeNoise(t *testing.T) {
	// Две равные по длине части: 40 и 50 дБ → энергетическое среднее около 47,4, не арифметическое 45.
	fn, ok := ComputeFacadeNoise([]NoiseSample{
		{LnDB: 40, OciLnDB: 20, OverlapM: 10},
		{LnDB: 50, OciLnDB: 35, OverlapM: 10},
	})
	if !ok {
		t.Fatal("результата нет")
	}
	want := 10 * math.Log10((math.Pow(10, 4)+math.Pow(10, 5))/2)
	if math.Abs(fn.LnDB-want) > 1e-9 || fn.Level != NoiseModerate || fn.OciLnDB != 35 || fn.Segments != 2 {
		t.Errorf("%+v, want Ln %v moderate oci 35 segments 2", fn, want)
	}

	// Длинный громкий сегмент перевешивает короткий тихий.
	fn, _ = ComputeFacadeNoise([]NoiseSample{{LnDB: 35, OverlapM: 1}, {LnDB: 60, OverlapM: 20}})
	if fn.Level != NoiseLoud {
		t.Errorf("уровень %s, want loud", fn.Level)
	}

	// Нулевое перекрытие не считается; без сегментов результата нет.
	if _, ok := ComputeFacadeNoise([]NoiseSample{{LnDB: 60, OverlapM: 0}}); ok {
		t.Error("сегмент без перекрытия дал результат")
	}
	if _, ok := ComputeFacadeNoise(nil); ok {
		t.Error("пустой список дал результат")
	}
}

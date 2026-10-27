package metrics

import "context"

// Store — чтение данных для метрик. Реализация — adapter/repository/postgres, только SELECT.
type Store interface {
	DemoHotels(ctx context.Context) (int, error)
	HotelFacts(ctx context.Context) ([]HotelFact, error)
	Questions(ctx context.Context) ([]QuestionRow, error)
	FacadeNoise(ctx context.Context) ([]FacadeNoise, error)
}

// Load собирает вход расчёта из хранилища, разметки и реестра.
func Load(ctx context.Context, st Store, labels []HotelLabel, chars []CharInfo) (Input, error) {
	in := Input{Labels: labels, Characteristics: chars}
	var err error
	if in.DemoHotels, err = st.DemoHotels(ctx); err != nil {
		return in, err
	}
	if in.Facts, err = st.HotelFacts(ctx); err != nil {
		return in, err
	}
	if in.Questions, err = st.Questions(ctx); err != nil {
		return in, err
	}
	in.Facades, err = st.FacadeNoise(ctx)
	return in, err
}

package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/repository/postgres"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// runSearchDemo печатает топ-5 по пяти запросам проверки поиска. Спрос не пишется: прогоны не должны двигать
// ценность вопросов отельерам.
func runSearchDemo(ctx context.Context, reg domain.Registry, pool *pgxpool.Pool, region domain.Region, hotelID int64) error {
	roomSvc := &rooms.Service{Registry: reg, Store: postgres.NewRoomStore(pool, reg.Rules())}
	svc := &search.Service{Registry: reg, Store: postgres.NewSearchStore(pool), Rooms: roomSvc, RegionID: region.ID}
	if hotelID != 0 {
		return printHotelSeaView(ctx, svc, hotelID)
	}
	for _, dq := range search.DemoQueries {
		q := dq.Query
		q.Limit = 5
		res, err := svc.Search(ctx, q)
		if err != nil {
			return fmt.Errorf("%q: %w", dq.Text, err)
		}
		fmt.Printf("\n=== %s (рассмотрено отелей %d, отфильтровано %d)\n", dq.Text, res.Considered, res.Dropped)
		for i, h := range res.Hotels {
			fmt.Printf("%d. [%d] %s — оценка %.2f%s\n", i+1, h.HotelID, h.Name, h.Best.Score, violationNote(h.Best))
			printVariant("   лучший", h.Best)
			if h.Alt != nil {
				printVariant("   альтернатива", *h.Alt)
			}
			printConflicts(h)
		}
	}
	return nil
}

func printConflicts(h domain.HotelResult) {
	for _, c := range h.OtherConflicts {
		fmt.Printf("   конфликт в другой категории «%s», %s: %s\n", c.Category, c.Key, c.Description)
	}
}

func violationNote(v domain.Variant) string {
	if len(v.Violations) == 0 {
		return ""
	}
	return " НАРУШЕНО: " + strings.Join(v.Violations, ", ")
}

func printVariant(label string, v domain.Variant) {
	g := ""
	if v.Guaranteed {
		g = ", гарантия"
	}
	fmt.Printf("%s: %s, %d €, оценка %.2f%s\n", label, v.Name, v.PriceEUR, v.Score, g)
	if label != "   лучший" {
		return
	}
	for _, e := range v.Evals {
		line := fmt.Sprintf("      %s [%s] %s", e.Key, e.Importance, e.Status)
		switch e.Status {
		case domain.StatusUnknown:
		default:
			line += fmt.Sprintf(": значение %v, p %.2f (без пожелания %.2f", e.Value, e.P, e.PBase)
			if e.Wish != nil {
				line += fmt.Sprintf(", с пожеланием «%s» %.2f", wishLine(e.Wish), e.PWish)
			}
			line += fmt.Sprintf("), уверенность %.2f", e.Confidence)
		}
		if e.Caveat != "" {
			line += " — " + e.Caveat
		}
		if len(e.Sources) > 0 {
			var ss []string
			for _, s := range e.Sources {
				ss = append(ss, fmt.Sprintf("%s/%s %s", s.AuthorType, s.Source, s.ObservedAt.Format("2006-01-02")))
			}
			line += " [" + strings.Join(ss, "; ") + "]"
		}
		fmt.Println(line)
	}
}

func wishLine(w *domain.VariantWish) string {
	if w.MinFloor > 0 {
		return fmt.Sprintf("фасад %d, этаж %d+", w.FacadeID, w.MinFloor)
	}
	return fmt.Sprintf("фасад %d", w.FacadeID)
}

// printHotelSeaView показывает, как поиск «вид на море» (must, без бюджета) оценивает один отель: название категории гарантии не даёт.
func printHotelSeaView(ctx context.Context, svc *search.Service, hotelID int64) error {
	res, err := svc.Search(ctx, domain.Query{Wishes: []domain.Desire{{Key: "sea_view", Importance: domain.Must}}, Limit: domain.MaxSearchLimit})
	if err != nil {
		return err
	}
	for i, h := range res.Hotels {
		if h.HotelID != hotelID {
			continue
		}
		fmt.Printf("[%d] %s — место %d из %d, оценка %.2f%s\n", h.HotelID, h.Name, i+1, res.Considered, h.Best.Score, violationNote(h.Best))
		printVariant("   лучший", h.Best)
		if h.Alt != nil {
			printVariant("   альтернатива", *h.Alt)
		}
		printConflicts(h)
		return nil
	}
	return fmt.Errorf("отель %d не попал в выдачу (не демо-отель или нет категорий)", hotelID)
}

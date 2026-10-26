// Package openmeteo — источник климата: Open-Meteo Historical Weather (ERA5) и Marine (температура моря).
// Данные открыты по лицензии CC BY 4.0, ключ не нужен (бесплатный некоммерческий уровень).
package openmeteo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulmach/orb"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
)

const (
	archiveURL = "https://archive-api.open-meteo.com/v1/archive"
	marineURL  = "https://marine-api.open-meteo.com/v1/marine"
	userAgent  = "tima-mechty-hackathon/0.1 (+https://github.com/ostrovok-hackathon-2026/tima-mechty)"
	timezone   = "Europe/Madrid"
)

// Client запрашивает суточную погоду и температуру моря.
type Client struct {
	ArchiveURL string
	MarineURL  string
	HTTP       *http.Client
	// RawDir — куда сохранять сырые ответы (data/raw); пусто — не сохранять.
	RawDir string
	// Backoff — пауза между попытками; в тестах обнуляется.
	Backoff time.Duration
}

func NewClient(rawDir string) *Client {
	return &Client{ArchiveURL: archiveURL, MarineURL: marineURL, HTTP: &http.Client{Timeout: 2 * time.Minute}, RawDir: rawDir, Backoff: 5 * time.Second}
}

type dailyResponse struct {
	Daily struct {
		Time     []string   `json:"time"`
		Mean     []*float64 `json:"temperature_2m_mean"`
		Max      []*float64 `json:"temperature_2m_max"`
		Min      []*float64 `json:"temperature_2m_min"`
		Precip   []*float64 `json:"precipitation_sum"`
		Sunshine []*float64 `json:"sunshine_duration"`
	} `json:"daily"`
}

type marineResponse struct {
	Hourly struct {
		Time []string   `json:"time"`
		SST  []*float64 `json:"sea_surface_temperature"`
	} `json:"hourly"`
}

// Daily возвращает суточные значения с from по to включительно для точки: воздух, осадки, солнце и температура моря
// (за дни, когда Marine API её отдаёт; иначе NaN).
func (c *Client) Daily(ctx context.Context, p orb.Point, from, to time.Time) ([]domain.ClimateDay, error) {
	q := url.Values{
		"latitude": {fmt.Sprint(p[1])}, "longitude": {fmt.Sprint(p[0])},
		"start_date": {from.Format("2006-01-02")}, "end_date": {to.Format("2006-01-02")},
		"timezone": {timezone},
	}
	weatherQ := cloneWith(q, "daily", "temperature_2m_mean,temperature_2m_max,temperature_2m_min,precipitation_sum,sunshine_duration")
	body, err := c.get(ctx, "archive", c.ArchiveURL, weatherQ)
	if err != nil {
		return nil, fmt.Errorf("архив погоды: %w", err)
	}
	var w dailyResponse
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("разбор архива погоды: %w", err)
	}
	marineQ := cloneWith(q, "hourly", "sea_surface_temperature")
	body, err = c.get(ctx, "marine", c.MarineURL, marineQ)
	if err != nil {
		return nil, fmt.Errorf("морские данные: %w", err)
	}
	var m marineResponse
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("разбор морских данных: %w", err)
	}
	return mergeDays(w, m), nil
}

func cloneWith(q url.Values, k, v string) url.Values {
	out := url.Values{}
	for key, vals := range q {
		out[key] = vals
	}
	out.Set(k, v)
	return out
}

func mergeDays(w dailyResponse, m marineResponse) []domain.ClimateDay {
	sea := map[string][2]float64{} // дата → сумма, число часов
	for i, t := range m.Hourly.Time {
		if i >= len(m.Hourly.SST) || m.Hourly.SST[i] == nil || len(t) < 10 {
			continue
		}
		acc := sea[t[:10]]
		sea[t[:10]] = [2]float64{acc[0] + *m.Hourly.SST[i], acc[1] + 1}
	}
	val := func(s []*float64, i int) float64 {
		if i >= len(s) || s[i] == nil {
			return math.NaN()
		}
		return *s[i]
	}
	out := make([]domain.ClimateDay, 0, len(w.Daily.Time))
	for i, ds := range w.Daily.Time {
		date, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		d := domain.ClimateDay{
			Date: date, MeanC: val(w.Daily.Mean, i), MaxC: val(w.Daily.Max, i), MinC: val(w.Daily.Min, i),
			PrecipMM: val(w.Daily.Precip, i), SunHours: val(w.Daily.Sunshine, i) / 3600, SeaC: math.NaN(),
		}
		if acc, ok := sea[ds]; ok && acc[1] >= 12 { // не меньше половины суток
			d.SeaC = acc[0] / acc[1]
		}
		out = append(out, d)
	}
	return out
}

// get выполняет GET с повторами на 429 и 5xx.
func (c *Client) get(ctx context.Context, label, base string, q url.Values) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.Backoff * time.Duration(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case err != nil:
			lastErr = err
		case resp.StatusCode == http.StatusOK:
			c.saveRaw(label, q, body)
			return body, nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		default:
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}
		log.Printf("open-meteo %s: %v", label, lastErr)
	}
	return nil, lastErr
}

func (c *Client) saveRaw(label string, q url.Values, body []byte) {
	if c.RawDir == "" {
		return
	}
	if err := os.MkdirAll(c.RawDir, 0o755); err != nil {
		return
	}
	name := fmt.Sprintf("openmeteo_%s_%s_%s_%s.json", label, q.Get("latitude"), q.Get("longitude"), time.Now().UTC().Format("20060102"))
	if err := os.WriteFile(filepath.Join(c.RawDir, strings.ReplaceAll(name, "..", ".")), body, 0o644); err != nil {
		log.Printf("сырые данные не сохранены: %v", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

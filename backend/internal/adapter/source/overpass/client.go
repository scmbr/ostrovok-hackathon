// Package overpass — источник геоданных OpenStreetMap через Overpass API.
package overpass

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultEndpoints — публичные зеркала Overpass; клиент перебирает их при ошибках.
var DefaultEndpoints = []string{
	"https://overpass-api.de/api/interpreter",
	"https://overpass.kumi.systems/api/interpreter",
	"https://maps.mail.ru/osm/tools/overpass/api/interpreter",
}

// Публичные серверы отвечают 406/429 на запросы без осмысленного User-Agent.
const userAgent = "tima-mechty-hackathon/0.1 (+https://github.com/ostrovok-hackathon-2026/tima-mechty)"

// Client выполняет запросы Overpass QL.
type Client struct {
	Endpoints []string
	HTTP      *http.Client
	// RawDir — куда сохранять сырые ответы (data/raw); пусто — не сохранять.
	RawDir string
	// Backoff — пауза между попытками; в тестах обнуляется.
	Backoff time.Duration
}

// NewClient создаёт клиент с зеркалами по умолчанию.
func NewClient(rawDir string) *Client {
	return &Client{
		Endpoints: DefaultEndpoints,
		HTTP:      &http.Client{Timeout: 5 * time.Minute},
		RawDir:    rawDir,
		Backoff:   5 * time.Second,
	}
}

// query отправляет запрос, перебирая зеркала по кругу (до двух проходов), и возвращает тело ответа.
// label используется в имени файла сырой выгрузки.
func (c *Client) query(ctx context.Context, label, ql string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2*len(c.Endpoints); attempt++ {
		endpoint := c.Endpoints[attempt%len(c.Endpoints)]
		body, retry, err := c.post(ctx, endpoint, ql)
		if err == nil {
			c.saveRaw(label, body)
			return body, nil
		}
		lastErr = fmt.Errorf("%s: %w", endpoint, err)
		log.Printf("overpass %s: %v", label, lastErr)
		if !retry {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.Backoff):
		}
	}
	return nil, fmt.Errorf("все зеркала Overpass недоступны: %w", lastErr)
}

// post возвращает тело; retry=true, если ошибку имеет смысл повторить на другом зеркале.
func (c *Client) post(ctx context.Context, endpoint, ql string) (body []byte, retry bool, err error) {
	form := url.Values{"data": {ql}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return body, false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("HTTP %d", resp.StatusCode)
	default:
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
}

func (c *Client) saveRaw(label string, body []byte) {
	if c.RawDir == "" {
		return
	}
	if err := os.MkdirAll(c.RawDir, 0o755); err != nil {
		log.Printf("сырые данные не сохранены: %v", err)
		return
	}
	name := fmt.Sprintf("overpass_%s_%s.json", label, time.Now().UTC().Format("20060102-150405"))
	if err := os.WriteFile(filepath.Join(c.RawDir, name), body, 0o644); err != nil {
		log.Printf("сырые данные не сохранены: %v", err)
		return
	}
	log.Printf("сырой ответ сохранён: %s", name)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

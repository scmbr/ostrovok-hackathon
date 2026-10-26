package llm

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

//go:embed all:cache/hotelier
var hotelierCacheFS embed.FS

// HotelierCacheDir — куда Save кладёт ответы живых прогонов интервьюера (относительно backend/).
const HotelierCacheDir = "internal/adapter/llm/cache/hotelier"

const hotelierCacheFile = "hotelier.json"

// ErrHotelierNotCached — в кеше интервьюера нет ответа на этот запрос; интервьюер переходит на шаблоны.
var ErrHotelierNotCached = errors.New("в режиме кеша нет сохранённого ответа LLM на этот запрос")

// Hotelier — LLM интервьюера отельера: формулировка вопроса и разбор ответа. Ответы живой модели записываются
// и воспроизводятся из кеша без ключа (ключ — хеш запроса).
type Hotelier struct {
	live  *Client // nil — только кеш
	model string

	mu       sync.Mutex
	replies  map[string]string // сохранённые ответы
	recorded map[string]string // записанные в этом прогоне
	record   bool
}

type hotelierFile struct {
	Model   string            `json:"model,omitempty"`
	Replies map[string]string `json:"replies"`
}

// NewHotelierCache — режим кеша: отвечает сохранёнными ответами (cache/hotelier/hotelier.json).
func NewHotelierCache() (*Hotelier, error) {
	h := &Hotelier{replies: map[string]string{}, recorded: map[string]string{}}
	data, err := fs.ReadFile(hotelierCacheFS, "cache/hotelier/"+hotelierCacheFile)
	if errors.Is(err, fs.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	var f hotelierFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", hotelierCacheFile, err)
	}
	for k, v := range f.Replies {
		h.replies[k] = v
	}
	return h, nil
}

// NewHotelierLive — режим живой модели; record включает запись ответов для кеша.
func NewHotelierLive(c *Client, record bool) (*Hotelier, error) {
	h, err := NewHotelierCache()
	if err != nil {
		return nil, err
	}
	h.live, h.model, h.record = c, c.Model, record
	return h, nil
}

func hotelierKey(system, user string) string {
	sum := sha256.Sum256([]byte(system + "\x00" + user))
	return hex.EncodeToString(sum[:])
}

func (h *Hotelier) Mode() string {
	if h.live != nil {
		return "llm"
	}
	return "cache"
}

func (h *Hotelier) Complete(ctx context.Context, system, user string) (string, error) {
	key := hotelierKey(system, user)
	if h.live == nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r, ok := h.replies[key]; ok {
			return r, nil
		}
		return "", ErrHotelierNotCached
	}
	text, err := h.live.Complete(ctx, registry.Request{System: system, User: user})
	if err != nil {
		return "", err
	}
	if h.record {
		h.mu.Lock()
		h.recorded[key] = text
		h.mu.Unlock()
	}
	return text, nil
}

// Save дописывает записанные ответы в dir/hotelier.json (прежние сохраняются); возвращает путь и число новых ответов.
func (h *Hotelier) Save(dir string) (string, int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recorded) == 0 {
		return "", 0, nil
	}
	merged := map[string]string{}
	for k, v := range h.replies {
		merged[k] = v
	}
	// Файл на диске может быть новее встроенного кеша (параллельные и последовательные прогоны одной сборки): дописываем к нему.
	if data, err := os.ReadFile(filepath.Join(dir, hotelierCacheFile)); err == nil {
		var onDisk hotelierFile
		if json.Unmarshal(data, &onDisk) == nil {
			for k, v := range onDisk.Replies {
				merged[k] = v
			}
		}
	}
	for k, v := range h.recorded {
		merged[k] = v
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	b, err := json.MarshalIndent(hotelierFile{Model: h.model, Replies: merged}, "", "  ")
	if err != nil {
		return "", 0, err
	}
	path := filepath.Join(dir, hotelierCacheFile)
	return path, len(h.recorded), os.WriteFile(path, append(b, '\n'), 0o644)
}

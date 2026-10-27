package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	adapterllm "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/llm"
	adapterregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/adapter/registry"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
	usecaseregistry "github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/registry"
)

type memCustom struct {
	mu   sync.Mutex
	rows []usecaseregistry.CustomRow
}

func (m *memCustom) ListCustom(context.Context) ([]usecaseregistry.CustomRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]usecaseregistry.CustomRow(nil), m.rows...), nil
}

func (m *memCustom) SaveCustom(_ context.Context, r usecaseregistry.CustomRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, r)
	return nil
}

func (m *memCustom) DeleteCustom(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rows {
		if r.Key == key {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			break
		}
	}
	return nil
}

// authoringForTest собирает добавление характеристик на настоящем встроенном реестре и заготовках LLM (режим без ключа);
// Reload перечитывает реестр так же, как работающий сервер, и кладёт его в atomic-указатель.
func authoringForTest(t *testing.T) (*usecaseregistry.Authoring, *atomic.Pointer[domain.Registry], *memCustom) {
	t.Helper()
	llm, err := adapterllm.NewCached()
	if err != nil {
		t.Fatal(err)
	}
	store := &memCustom{}
	var cur atomic.Pointer[domain.Registry]
	base, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	cur.Store(&base)
	auth := &usecaseregistry.Authoring{
		LLM: llm, Tools: compute.Specs, Store: store, Current: func() domain.Registry { return *cur.Load() },
		Reload: func(ctx context.Context) error {
			rows, _ := store.ListCustom(ctx)
			reg, err := LoadRegistryWith(rows)
			if err != nil {
				return err
			}
			cur.Store(&reg)
			return nil
		},
		Parse: adapterregistry.Parse, Marshal: adapterregistry.Marshal,
	}
	return auth, &cur, store
}

func parseKeys(reg domain.Registry, phrase string) []string {
	var keys []string
	for _, w := range domain.NewLexicon(reg).Parse(phrase).Wishes {
		keys = append(keys, w.Key)
	}
	return keys
}

// Сохранили характеристику, добавленную текстом, — разбор запроса гостя без LLM находит её без перезапуска.
func TestAddCharacteristicByTextIsVisibleWithoutRestart(t *testing.T) {
	ctx := context.Background()
	auth, cur, store := authoringForTest(t)

	d, err := auth.Draft(ctx, "есть ли рядом круглосуточный магазин")
	if err != nil {
		t.Fatal(err)
	}
	if d.Key != "night_shop" || len(d.Errors) != 0 || len(d.Synonyms) == 0 || !strings.Contains(d.YAML, "synonyms_origin: derived") {
		t.Fatalf("предпросмотр: %+v", d)
	}
	if got := parseKeys(*cur.Load(), "ночной магазин рядом"); contains(got, "night_shop") {
		t.Fatalf("до сохранения характеристика уже находится: %v", got)
	}

	c, err := auth.Save(ctx, d.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if c.SynonymsOrigin != domain.SynonymsDerived || len(store.rows) != 1 {
		t.Errorf("сохранено: %+v, строк %d", c, len(store.rows))
	}
	if _, ok := cur.Load().Get("night_shop"); !ok {
		t.Fatal("реестр не подменён")
	}
	if got := parseKeys(*cur.Load(), "ночной магазин рядом"); !contains(got, "night_shop") {
		t.Errorf("парсер не видит характеристику: %v", got)
	}
	// Тот же ключ второй раз — ошибка, а не тихая перезапись.
	if _, err := auth.Save(ctx, d.YAML); !errors.Is(err, usecaseregistry.ErrExists) {
		t.Errorf("повторное сохранение: %v", err)
	}
}

// Заготовка night_pharmacy уже есть в реестре: понятная ошибка, а не повторное добавление.
func TestDraftExistingCharacteristicIsReported(t *testing.T) {
	auth, _, _ := authoringForTest(t)
	_, err := auth.Draft(context.Background(), "есть ли рядом круглосуточная аптека")
	if !errors.Is(err, usecaseregistry.ErrExists) || !strings.Contains(err.Error(), "night_pharmacy") {
		t.Fatalf("ожидалась ErrExists про night_pharmacy: %v", err)
	}
}

// Правка синонимов вручную: пересечение с чужой характеристикой — ошибка с указанием, чья фраза; пустые синонимы — ошибка;
// отличие от предложенных кодом помечается edited.
func TestSaveChecksSynonyms(t *testing.T) {
	ctx := context.Background()
	auth, cur, _ := authoringForTest(t)
	d, err := auth.Draft(ctx, "есть ли рядом круглосуточный магазин")
	if err != nil {
		t.Fatal(err)
	}
	with := func(syn string) string {
		i := strings.Index(d.YAML, "synonyms:")
		j := strings.Index(d.YAML, "synonyms_origin:")
		return d.YAML[:i] + syn + d.YAML[j:]
	}

	var ve *usecaseregistry.ValidationError
	_, err = auth.Save(ctx, with("synonyms:\n  - ночная аптека\n"))
	if !errors.As(err, &ve) || !strings.Contains(ve.Error(), "night_pharmacy") {
		t.Errorf("пересечение с night_pharmacy: %v", err)
	}
	_, err = auth.Save(ctx, with("synonyms: []\n"))
	if !errors.As(err, &ve) || !strings.Contains(ve.Error(), "нет synonyms") {
		t.Errorf("пустые синонимы: %v", err)
	}
	if _, ok := cur.Load().Get("night_shop"); ok {
		t.Fatal("ошибочные варианты не должны сохраняться")
	}

	c, err := auth.Save(ctx, with("synonyms:\n  - круглосуточный магазин\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.SynonymsOrigin != domain.SynonymsEdited {
		t.Errorf("synonyms_origin = %q, ожидалось edited", c.SynonymsOrigin)
	}
}

// Подмена реестра под нагрузкой: читатели видят либо прежний, либо новый реестр целиком (go test -race).
func TestReloadUnderConcurrentReaders(t *testing.T) {
	ctx := context.Background()
	auth, cur, _ := authoringForTest(t)
	d, err := auth.Draft(ctx, "есть ли рядом круглосуточный магазин")
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					reg := *cur.Load()
					if _, ok := reg.Get("sea_view"); !ok {
						t.Error("встроенная характеристика пропала")
						return
					}
					_ = domain.NewLexicon(reg).Parse("тихий номер с видом на море")
				}
			}
		}()
	}
	if _, err := auth.Save(ctx, d.YAML); err != nil {
		t.Error(err)
	}
	close(stop)
	wg.Wait()
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Строка БД с ключом встроенной характеристики — ошибка загрузки реестра (старт сервера), а не тихая перезапись.
func TestLoadRegistryWithRejectsDuplicateKey(t *testing.T) {
	dup := "key: sea_view\ntitle: Дубль\ndescription: x\nlevel: hotel\nvalue_type: bool\nttl_days: 30\nask_hotelier: x\norigin: text\norigin_text: x\nadded_at: 2026-10-27\nsynonyms: [дубль]\n"
	_, err := LoadRegistryWith([]usecaseregistry.CustomRow{{Key: "sea_view", YAML: dup, OriginText: "x", AddedAt: "2026-10-27"}})
	if err == nil || !strings.Contains(err.Error(), "sea_view") {
		t.Fatalf("ожидалась ошибка про sea_view: %v", err)
	}
}

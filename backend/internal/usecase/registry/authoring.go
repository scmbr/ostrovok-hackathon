package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/compute"
)

// ErrExists — ключ новой характеристики уже есть в реестре (встроенной или добавленной раньше).
var ErrExists = errors.New("характеристика уже есть в реестре")

// CustomRow — характеристика, добавленная текстом, как она лежит в хранилище.
type CustomRow struct {
	Key, YAML, OriginText string
	AddedAt               string // YYYY-MM-DD
}

// CustomStore — порт хранилища добавленных характеристик (custom_characteristics).
type CustomStore interface {
	ListCustom(ctx context.Context) ([]CustomRow, error)
	SaveCustom(ctx context.Context, r CustomRow) error
	// DeleteCustom убирает характеристику: откат записи, после которой реестр не собрался.
	DeleteCustom(ctx context.Context, key string) error
}

// Authoring добавляет характеристику из интерфейса: текст → предпросмотр YAML с проверкой → сохранение в БД → подмена
// реестра в работающем сервере. LLM описывает, как получать характеристику, и не пишет значений фактов; синонимы для
// разбора запроса гостя без модели предлагает код (domain.DeriveSynonyms), подсказку add-characteristic это не меняет.
type Authoring struct {
	LLM   LLM
	Tools []compute.Spec
	Store CustomStore
	// Current — действующий реестр (меняется после Reload).
	Current func() domain.Registry
	// Reload перечитывает реестр (встроенный + БД) и подменяет его в работающем сервере.
	Reload  func(ctx context.Context) error
	Parse   func([]byte) (domain.Characteristic, error)
	Marshal func(domain.Characteristic) ([]byte, error)
	Now     func() time.Time

	mu sync.Mutex
}

// Draft — предпросмотр новой характеристики.
type Draft struct {
	YAML     string
	Key      string
	Title    string
	Synonyms []string
	Notes    []string // правки системы к ответу LLM и убранные синонимы
	Errors   []string // что мешает сохранить; YAML можно поправить
	Attempts int
}

func (a *Authoring) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Draft составляет характеристику по тексту и возвращает YAML с результатом проверки. Ключ, уже занятый в реестре,
// даёт ErrExists, остальные замечания к YAML — в Draft.Errors.
func (a *Authoring) Draft(ctx context.Context, text string) (Draft, error) {
	text = strings.TrimSpace(text)
	reg := a.Current()
	adder := &Adder{LLM: a.LLM, Registry: reg, Tools: a.Tools, Parse: a.Parse}
	res, err := adder.Add(ctx, text)
	if err != nil {
		if strings.Contains(err.Error(), "уже есть в реестре") {
			key, _, _ := strings.Cut(firstLine(err.Error()), ":")
			return Draft{}, fmt.Errorf("%w: %s", ErrExists, strings.TrimSpace(key))
		}
		return Draft{}, err
	}
	c := res.Characteristic
	c.Origin, c.OriginText, c.AddedAt = domain.OriginText, text, a.now().Format(time.DateOnly)
	d := Draft{Key: c.Key, Title: c.Title, Notes: append([]string(nil), res.Notes...), Attempts: res.Attempts}
	if len(c.Synonyms) == 0 {
		var skipped []string
		c.Synonyms, skipped = a.derive(reg, c.Key, c.Title, text)
		d.Notes = append(d.Notes, skipped...)
		c.SynonymsOrigin = domain.SynonymsDerived
	}
	d.Synonyms = c.Synonyms
	body, err := a.Marshal(c)
	if err != nil {
		return d, err
	}
	d.YAML = string(body)
	d.Errors = a.check(reg, c)
	return d, nil
}

// derive — синонимы, которые предлагает код: domain.DeriveSynonyms без фраз, пересекающихся с другими характеристиками
// реестра (они сломали бы разбор запроса); skipped — пояснения, что убрано и почему.
func (a *Authoring) derive(reg domain.Registry, key, title, text string) (syn, skipped []string) {
	for _, p := range domain.DeriveSynonyms(title, text) {
		if ov := domain.OverlapsWith(reg, key, p); len(ov) > 0 {
			skipped = append(skipped, fmt.Sprintf("синоним «%s» не предложен: пересекается с «%s» характеристики %s", p, ov[0].PhraseB, ov[0].KeyB))
			continue
		}
		syn = append(syn, p)
	}
	return syn, skipped
}

// firstLine — строка ошибки, где названа причина «уже есть в реестре» (ответ Adder многострочный).
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "уже есть в реестре") {
			return strings.TrimSpace(l)
		}
	}
	return s
}

// check проверяет характеристику c вместе со всем реестром reg: та же Validate, что на старте, плюс правила сохранения —
// синонимы не пустые (иначе гость без модели её текстом не найдёт).
func (a *Authoring) check(reg domain.Registry, c domain.Characteristic) []string {
	var out []string
	if err := ValidateOne(reg, c, a.Tools); err != nil {
		out = append(out, splitErrors(err)...)
	}
	// Действующий реестр уже прошёл Validate, поэтому любая ошибка общей проверки — следствие новой характеристики
	// (например, пересечение её синонимов с чужими).
	if merged, err := domain.NewRegistry(append(reg.All(), c)); err == nil {
		merged = merged.WithPoi(reg.Poi())
		if err := Validate(merged, a.Tools); err != nil {
			for _, m := range splitErrors(err) {
				if !contains(out, m) {
					out = append(out, m)
				}
			}
		}
	}
	if len(nonEmpty(c.Synonyms)) == 0 {
		out = append(out, c.Key+": нет synonyms — без них гость не найдёт характеристику текстом без LLM; предложенные фразы пересекаются с уже существующими, добавьте в YAML свои фразы гостя (например, «круглосуточное кафе»)")
	}
	return out
}

func splitErrors(err error) []string {
	var out []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if len(domain.PhraseWords(s)) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// ValidationError — YAML не прошёл проверку; Problems — все замечания.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string { return strings.Join(e.Problems, "; ") }

// Save разбирает отредактированный YAML, проверяет его вместе с реестром, записывает в БД и подменяет реестр в
// работающем сервере. Ключ, уже занятый в реестре, — ErrExists. Если синонимы отличаются от предложенных кодом,
// synonyms_origin становится edited.
func (a *Authoring) Save(ctx context.Context, yamlText string) (domain.Characteristic, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := a.Parse([]byte(yamlText))
	if err != nil {
		return c, &ValidationError{Problems: []string{err.Error()}}
	}
	if c.Origin != domain.OriginText {
		return c, &ValidationError{Problems: []string{"из интерфейса добавляются только характеристики с origin: text"}}
	}
	reg := a.Current()
	if _, exists := reg.Get(c.Key); exists {
		return c, fmt.Errorf("%w: %s", ErrExists, c.Key)
	}
	if c.AddedAt == "" {
		c.AddedAt = a.now().Format(time.DateOnly)
	}
	derived, _ := a.derive(reg, c.Key, c.Title, c.OriginText)
	if !sameStrings(derived, c.Synonyms) {
		c.SynonymsOrigin = domain.SynonymsEdited
	} else if c.SynonymsOrigin == "" {
		c.SynonymsOrigin = domain.SynonymsDerived
	}
	if problems := a.check(reg, c); len(problems) > 0 {
		return c, &ValidationError{Problems: problems}
	}
	body, err := a.Marshal(c)
	if err != nil {
		return c, err
	}
	if err := a.Store.SaveCustom(ctx, CustomRow{Key: c.Key, YAML: string(body), OriginText: c.OriginText, AddedAt: c.AddedAt}); err != nil {
		return c, fmt.Errorf("запись характеристики: %w", err)
	}
	if err := a.Reload(ctx); err != nil {
		// Запись, с которой реестр не собирается, остановила бы сервер на старте: откатываем её.
		if derr := a.Store.DeleteCustom(ctx, c.Key); derr != nil {
			return c, fmt.Errorf("реестр после сохранения: %w (откат записи: %v)", err, derr)
		}
		return c, fmt.Errorf("реестр после сохранения: %w", err)
	}
	return c, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

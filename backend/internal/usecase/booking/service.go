// Пакет booking — путь гостя от выдачи до брони: шортлист, умное сравнение, контракт ожиданий, «спросить отель» и
// бронь с пожеланием. Сравнение и контракт строятся кодом из структур поиска (domain.Compare, domain.ContractLines),
// без LLM. Описание — docs/compare.md.
package booking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/agent"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/hotelier"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/search"
)

// AgentName — запись журнала: решения гостевого пути идут в тот же журнал, что и диалог.
const AgentName = "guest_dialog"

// MaxShortlist — сколько отелей держит шортлист.
const MaxShortlist = 10

// ModeRule — режим записи журнала: решение принято правилом, не моделью.
const ModeRule = "rule"

var (
	// ErrInvalid — запрос не прошёл проверку.
	ErrInvalid = errors.New("некорректный запрос")
	// ErrNotFound — отеля, категории или брони нет.
	ErrNotFound = errors.New("не найдено")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Service — гостевой путь. Demand == nil — спрос не пишется.
type Service struct {
	Registry domain.Registry
	Search   Searcher
	Sessions SessionStore
	Hotelier Hotelier
	Demand   DemandRecorder
	Bookings Store
	Trace    TraceWriter
	RegionID int64
}

// session — часть состояния диалога, которая нужна этому пакету. Остальное хранится как есть.
type session struct {
	Wishes    []domain.Desire `json:"wishes"`
	Filters   domain.Filters  `json:"filters"`
	Shortlist []int64         `json:"shortlist"`
}

func (s *Service) load(ctx context.Context, id string) (session, map[string]json.RawMessage, error) {
	var v session
	raw := map[string]json.RawMessage{}
	b, ok, err := s.Sessions.LoadState(ctx, id)
	if err != nil {
		return v, raw, fmt.Errorf("сессия: %w", err)
	}
	if ok {
		if err := json.Unmarshal(b, &raw); err != nil {
			return v, raw, fmt.Errorf("состояние сессии: %w", err)
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return v, raw, fmt.Errorf("состояние сессии: %w", err)
		}
	}
	return v, raw, nil
}

func (s *Service) saveShortlist(ctx context.Context, id string, raw map[string]json.RawMessage, list []int64) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	raw["shortlist"] = b
	out, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return s.Sessions.SaveState(ctx, id, out)
}

// Shown — отель шортлиста.
type Shown struct {
	ID   int64
	Name string
}

func (s *Service) names(ctx context.Context, ids []int64) ([]Shown, error) {
	cands, err := s.Search.Hotels(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]string{}
	for _, c := range cands {
		byID[c.ID] = c.Name
	}
	out := []Shown{}
	for _, id := range ids {
		if n, ok := byID[id]; ok {
			out = append(out, Shown{ID: id, Name: n})
		}
	}
	return out, nil
}

func checkSession(id string) error {
	if id == "" || len(id) > 64 {
		return invalid("session_id обязателен, до 64 символов")
	}
	return nil
}

// Shortlist возвращает шортлист сессии.
func (s *Service) Shortlist(ctx context.Context, sessionID string) ([]Shown, error) {
	if err := checkSession(sessionID); err != nil {
		return nil, err
	}
	v, _, err := s.load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.names(ctx, v.Shortlist)
}

// AddToShortlist добавляет отель; повтор не дублирует.
func (s *Service) AddToShortlist(ctx context.Context, sessionID string, hotelID int64) ([]Shown, error) {
	if err := checkSession(sessionID); err != nil {
		return nil, err
	}
	v, raw, err := s.load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	cands, err := s.Search.Hotels(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(cands, func(c search.Candidate) bool { return c.ID == hotelID }) {
		return nil, fmt.Errorf("%w: отель %d", ErrNotFound, hotelID)
	}
	if !slices.Contains(v.Shortlist, hotelID) {
		if len(v.Shortlist) >= MaxShortlist {
			return nil, invalid("в шортлисте не больше %d отелей", MaxShortlist)
		}
		v.Shortlist = append(v.Shortlist, hotelID)
		if err := s.saveShortlist(ctx, sessionID, raw, v.Shortlist); err != nil {
			return nil, err
		}
	}
	return s.names(ctx, v.Shortlist)
}

// RemoveFromShortlist убирает отель; отсутствующий — не ошибка.
func (s *Service) RemoveFromShortlist(ctx context.Context, sessionID string, hotelID int64) ([]Shown, error) {
	if err := checkSession(sessionID); err != nil {
		return nil, err
	}
	v, raw, err := s.load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if slices.Contains(v.Shortlist, hotelID) {
		v.Shortlist = slices.DeleteFunc(v.Shortlist, func(id int64) bool { return id == hotelID })
		if err := s.saveShortlist(ctx, sessionID, raw, v.Shortlist); err != nil {
			return nil, err
		}
	}
	return s.names(ctx, v.Shortlist)
}

// query собирает запрос гостя: явные желания запроса или желания сессии.
func (s *Service) query(ctx context.Context, sessionID string, wishes []domain.Desire) (domain.Query, error) {
	if len(wishes) > 0 {
		return domain.Query{Wishes: wishes, SessionID: sessionID}, nil
	}
	if err := checkSession(sessionID); err != nil {
		return domain.Query{}, invalid("нужны session_id или wishes")
	}
	v, _, err := s.load(ctx, sessionID)
	if err != nil {
		return domain.Query{}, err
	}
	return domain.Query{Wishes: v.Wishes, Filters: v.Filters, SessionID: sessionID}, nil
}

func (s *Service) eval(ctx context.Context, hotelID int64, q domain.Query) (search.HotelVariants, domain.Query, error) {
	hv, nq, err := s.Search.EvalHotel(ctx, hotelID, q)
	var ve *search.ValidationError
	switch {
	case errors.Is(err, search.ErrNotFound):
		return hv, nq, fmt.Errorf("%w: отель %d или его категории", ErrNotFound, hotelID)
	case errors.As(err, &ve):
		return hv, nq, fmt.Errorf("%w: %s", ErrInvalid, ve.Msg)
	}
	return hv, nq, err
}

func (s *Service) trace(ctx context.Context, sessionID string, hotelID int64, decision, tool string, in, out map[string]any) error {
	if s.Trace == nil || sessionID == "" {
		return nil
	}
	return s.Trace.AppendTrace(ctx, agent.TraceStep{Agent: AgentName, SessionID: sessionID, HotelID: hotelID, Mode: ModeRule,
		Step: 1, Decision: decision, Tool: tool, Input: in, Output: out})
}

// CompareRequest — что сравнивать: 2–3 отеля (HotelIDs) или 2–3 категории одного отеля (HotelID + CategoryIDs).
// Желания берутся из запроса или из сессии.
type CompareRequest struct {
	SessionID   string
	Wishes      []domain.Desire
	HotelIDs    []int64
	HotelID     int64
	CategoryIDs []int64
}

// Compared — результат сравнения.
type Compared struct {
	Query      domain.Query
	Comparison domain.Comparison
	// Categories — сравнение категорий одного отеля, иначе лучших вариантов разных отелей.
	Categories bool
	HotelID    int64
}

func distinct(ids []int64) bool {
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func sizeOK(n int) bool { return n >= 2 && n <= domain.MaxCompareSides }

// Compare сравнивает лучшие для гостя варианты.
func (s *Service) Compare(ctx context.Context, req CompareRequest) (Compared, error) {
	q, err := s.query(ctx, req.SessionID, req.Wishes)
	if err != nil {
		return Compared{}, err
	}
	var out Compared
	var sides []domain.CompareSide
	switch {
	case len(req.HotelIDs) > 0:
		if !sizeOK(len(req.HotelIDs)) || !distinct(req.HotelIDs) {
			return out, invalid("для сравнения нужно от 2 до %d разных отелей", domain.MaxCompareSides)
		}
		for _, id := range req.HotelIDs {
			hv, nq, err := s.eval(ctx, id, q)
			if err != nil {
				return out, err
			}
			q = nq
			sides = append(sides, domain.CompareSide{HotelID: id, Label: hv.Result.Name, Variant: hv.Result.Best, Conflicts: hv.Result.OtherConflicts})
		}
	case req.HotelID != 0 && len(req.CategoryIDs) > 0:
		if !sizeOK(len(req.CategoryIDs)) || !distinct(req.CategoryIDs) {
			return out, invalid("для сравнения нужно от 2 до %d разных категорий", domain.MaxCompareSides)
		}
		hv, nq, err := s.eval(ctx, req.HotelID, q)
		if err != nil {
			return out, err
		}
		q = nq
		for _, cid := range req.CategoryIDs {
			i := slices.IndexFunc(hv.Variants, func(v domain.Variant) bool { return v.CategoryID == cid })
			if i < 0 {
				return out, fmt.Errorf("%w: категория %d отеля %d", ErrNotFound, cid, req.HotelID)
			}
			sides = append(sides, domain.CompareSide{HotelID: req.HotelID, Label: hv.Variants[i].Name, Variant: hv.Variants[i]})
		}
		out.Categories, out.HotelID = true, req.HotelID
	default:
		return out, invalid("нужны hotel_ids (2–%d) или hotel_id с category_ids", domain.MaxCompareSides)
	}
	out.Query = q
	out.Comparison = domain.Compare(s.Registry, q.Wishes, sides)
	labels := make([]string, len(sides))
	for i, sd := range sides {
		labels[i] = sd.Label
	}
	winner := ""
	if w := out.Comparison.Winner; w >= 0 {
		winner = sides[w].Label
	}
	err = s.trace(ctx, req.SessionID, req.HotelID, fmt.Sprintf("сравнение: %s — %s", strings.Join(labels, " / "), out.Comparison.Verdict), "compare",
		map[string]any{"sides": labels, "categories": out.Categories, "wishes": wishKeys(q.Wishes)},
		map[string]any{"winner": winner, "verdict": out.Comparison.Verdict, "differences": len(out.Comparison.Differences),
			"same": len(out.Comparison.Same), "uncertain": len(out.Comparison.Uncertain)})
	return out, err
}

func wishKeys(ws []domain.Desire) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Key
	}
	return out
}

// Contract — контракт ожиданий по выбранному варианту.
type Contract struct {
	Query      domain.Query
	HotelID    int64
	HotelName  string
	CategoryID int64
	Variant    domain.Variant
	Lines      []domain.ContractLine
	// Offer — пожелание к брони, которое повышает шанс; nil — пожелание не нужно или невозможно.
	Offer *domain.VariantWish
	// WithWish — контракт посчитан с пожеланием.
	WithWish bool
}

// ContractRequest — для какого варианта строить контракт.
type ContractRequest struct {
	SessionID  string
	Wishes     []domain.Desire
	HotelID    int64
	CategoryID int64
	WithWish   bool
}

func (s *Service) contract(ctx context.Context, req ContractRequest) (Contract, error) {
	q, err := s.query(ctx, req.SessionID, req.Wishes)
	if err != nil {
		return Contract{}, err
	}
	if req.HotelID == 0 || req.CategoryID == 0 {
		return Contract{}, invalid("нужны hotel_id и category_id")
	}
	hv, q, err := s.eval(ctx, req.HotelID, q)
	if err != nil {
		return Contract{}, err
	}
	i := slices.IndexFunc(hv.Variants, func(v domain.Variant) bool { return v.CategoryID == req.CategoryID })
	if i < 0 {
		return Contract{}, fmt.Errorf("%w: категория %d отеля %d", ErrNotFound, req.CategoryID, req.HotelID)
	}
	v := hv.Variants[i]
	c := Contract{Query: q, HotelID: req.HotelID, HotelName: hv.Result.Name, CategoryID: req.CategoryID, Variant: v, WithWish: req.WithWish}
	for _, e := range v.Evals {
		if e.Wish != nil {
			c.Offer = e.Wish
			break
		}
	}
	if req.WithWish && c.Offer == nil {
		return c, invalid("для этой категории пожелание не нужно или невозможно: гарантия есть либо подходящих номеров нет")
	}
	c.Lines = domain.ContractLines(s.Registry, v, req.WithWish)
	return c, nil
}

// Contract строит контракт ожиданий: список важного для гостя с честной степенью уверенности по каждому пункту.
func (s *Service) Contract(ctx context.Context, req ContractRequest) (Contract, error) {
	c, err := s.contract(ctx, req)
	if err != nil {
		return c, err
	}
	var asks []string
	for _, l := range c.Lines {
		if l.CanAsk {
			asks = append(asks, l.Key)
		}
	}
	err = s.trace(ctx, req.SessionID, req.HotelID, fmt.Sprintf("контракт ожиданий: %s, «%s», пунктов %d, можно спросить отель по %d", c.HotelName, c.Variant.Name, len(c.Lines), len(asks)), "contract",
		map[string]any{"category_id": req.CategoryID, "with_wish": req.WithWish}, map[string]any{"ask": asks, "score": c.Variant.Score})
	return c, err
}

// AskRequest — «спросить отель» по пункту контракта или сравнения.
type AskRequest struct {
	SessionID string
	HotelID   int64
	Key       string
}

// AskResult — итог: вопрос отельеру (созданный или найденный) и ответ гостю. Бронь не блокируется.
type AskResult struct {
	Ask     hotelier.Ask
	Message string
}

// AskHotel создаёт или находит вопрос отельеру по характеристике. Вопрос про характеристику отеля, а не от имени гостя;
// дубль живого вопроса не создаётся. Спрос по ключу учитывается один раз за сессию.
func (s *Service) AskHotel(ctx context.Context, req AskRequest) (AskResult, error) {
	if err := checkSession(req.SessionID); err != nil {
		return AskResult{}, err
	}
	c, ok := s.Registry.Get(req.Key)
	if !ok {
		return AskResult{}, invalid("характеристики %q нет в реестре", req.Key)
	}
	if req.HotelID == 0 {
		return AskResult{}, invalid("нужен hotel_id")
	}
	if s.Demand != nil {
		if err := s.Demand.Record(ctx, s.RegionID, req.SessionID, []string{req.Key}); err != nil {
			return AskResult{}, fmt.Errorf("спрос: %w", err)
		}
	}
	ask, err := s.Hotelier.AskAbout(ctx, req.HotelID, req.Key)
	if err != nil {
		return AskResult{}, err
	}
	res := AskResult{Ask: ask}
	switch {
	case ask.Question == nil:
		res.Message = ask.Note
	case ask.Sent && ask.Created:
		res.Message = fmt.Sprintf("Спросили отель про «%s». Ответ сохранится в профиле отеля для всех гостей; бронь это не блокирует.", c.Title)
	case ask.Sent:
		res.Message = fmt.Sprintf("Отель уже спрашивали про «%s», ответа ещё нет. Бронь не блокируется.", c.Title)
	default:
		res.Message = fmt.Sprintf("Вопрос про «%s» поставлен в очередь отеля: %s.", c.Title, ask.Note)
	}
	qid := int64(0)
	if ask.Question != nil {
		qid = ask.Question.ID
	}
	err = s.trace(ctx, req.SessionID, req.HotelID, "спросить отель: "+c.Key+" — "+res.Message, "ask_hotelier",
		map[string]any{"characteristic": req.Key}, map[string]any{"question_id": qid, "created": ask.Created, "reused": ask.Reused, "sent": ask.Sent})
	return res, err
}

// Статусы пожелания в брони.
const (
	WishWaiting   = "waiting"   // ждём подтверждения отеля
	WishConfirmed = "confirmed" // отель подтвердил
	WishRefused   = "refused"   // отель не гарантирует
)

// BookRequest — бронь. Снимок контракта строит сервер по состоянию на момент брони.
type BookRequest struct {
	SessionID  string
	Wishes     []domain.Desire
	HotelID    int64
	CategoryID int64
	// FacadeID и MinFloor — пожелание, если гость бронирует с пожеланием; FacadeID == 0 — без него.
	FacadeID int64
	MinFloor int
}

// Booked — созданная бронь.
type Booked struct {
	ID         int64
	Contract   Contract
	WishStatus string // пусто, если пожелания нет
	WishText   string
	Question   *hotelier.Question
	Reused     bool
}

// Book создаёт заявку (оплаты нет). С пожеланием отельеру уходит вопрос wish_guarantee формата confirm; он один на
// отель и вне недельного бюджета. Если отельер уже отвечал, статус пожелания известен сразу.
func (s *Service) Book(ctx context.Context, req BookRequest) (Booked, error) {
	if err := checkSession(req.SessionID); err != nil {
		return Booked{}, err
	}
	withWish := req.FacadeID != 0
	c, err := s.contract(ctx, ContractRequest{SessionID: req.SessionID, Wishes: req.Wishes, HotelID: req.HotelID, CategoryID: req.CategoryID, WithWish: withWish})
	if err != nil {
		return Booked{}, err
	}
	rec := Record{SessionID: req.SessionID, HotelID: req.HotelID, CategoryID: req.CategoryID, Expectations: c.Lines}
	out := Booked{Contract: c}
	if withWish {
		o := c.Offer
		if o.FacadeID != req.FacadeID || o.MinFloor != req.MinFloor {
			return out, invalid("пожелание должно совпадать с предложенным: фасад %d, этаж %d+", o.FacadeID, o.MinFloor)
		}
		out.WishText = domain.WishText(*o)
		ask, err := s.Hotelier.ConfirmWish(ctx, req.HotelID, c.Variant.Name, out.WishText)
		if err != nil {
			return out, err
		}
		out.Question, out.Reused = ask.Question, ask.Reused
		switch {
		case ask.Answered && ask.Guarantees:
			out.WishStatus = WishConfirmed
		case ask.Answered:
			out.WishStatus = WishRefused
		default:
			out.WishStatus = WishWaiting
		}
		rec.Wish = map[string]any{"facade_id": o.FacadeID, "min_floor": o.MinFloor, "text": out.WishText, "probability": o.Probability}
		if ask.Question != nil {
			rec.WishQuestionID = ask.Question.ID
		}
	}
	saved, err := s.Bookings.CreateBooking(ctx, rec)
	if err != nil {
		return out, fmt.Errorf("бронь: %w", err)
	}
	out.ID = saved.ID
	qid := int64(0)
	if out.Question != nil {
		qid = out.Question.ID
	}
	err = s.trace(ctx, req.SessionID, req.HotelID, fmt.Sprintf("бронь %d: %s, «%s»%s", saved.ID, c.HotelName, c.Variant.Name, wishSuffix(out)), "book",
		map[string]any{"category_id": req.CategoryID, "wish": out.WishText},
		map[string]any{"booking_id": saved.ID, "wish_status": out.WishStatus, "question_id": qid, "question_reused": out.Reused})
	return out, err
}

func wishSuffix(b Booked) string {
	if b.WishText == "" {
		return ""
	}
	return fmt.Sprintf(" с пожеланием «%s» (%s)", b.WishText, b.WishStatus)
}

// BookingView — бронь с актуальным статусом пожелания: ответ отеля мог прийти после брони.
type BookingView struct {
	Record     Record
	WishStatus string
}

// Booking читает бронь; статус пожелания берётся из текущего ответа отеля.
func (s *Service) Booking(ctx context.Context, id int64) (BookingView, error) {
	r, err := s.Bookings.Booking(ctx, id)
	if err != nil {
		return BookingView{}, err
	}
	v := BookingView{Record: r}
	if r.Wish == nil {
		return v, nil
	}
	answered, yes, err := s.Hotelier.WishAnswer(ctx, r.HotelID)
	if err != nil {
		return v, err
	}
	switch {
	case !answered:
		v.WishStatus = WishWaiting
	case yes:
		v.WishStatus = WishConfirmed
	default:
		v.WishStatus = WishRefused
	}
	return v, nil
}

// Пакет search — поиск отелей по структурному запросу гостя: лучший вариант номера для каждого отеля, оценка желаний
// с объяснением, «похожий на этот». Формулы — в domain (search.go), описание — docs/search.md.
package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/usecase/rooms"
)

// ErrNotFound — отеля нет.
var ErrNotFound = errors.New("отель не найден")

// ValidationError — запрос не прошёл проверку по схеме реестра.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Candidate — отель, участвующий в поиске, с мок-карточкой.
type Candidate struct {
	ID       int64
	Name     string
	District string
	Card     domain.Card
	Lat, Lon float64
}

// FactView — факт со свидетельствами, из которых он сложен.
type FactView struct {
	Fact     domain.Fact
	Evidence []rooms.EvidenceView
}

// Store — хранилище поиска. Реализация — adapter/repository/postgres.
type Store interface {
	// Candidates — отели региона, в которых можно искать: демо-отели с размещёнными категориями.
	Candidates(ctx context.Context, regionID int64) ([]Candidate, error)
	Candidate(ctx context.Context, hotelID int64) (Candidate, error)
	// HotelFacts — активные факты отеля и его района со свидетельствами и открытым конфликтом.
	HotelFacts(ctx context.Context, hotelID int64) ([]FactView, error)
}

// RoomsSource — категории номеров отеля с вероятностями (rooms.Service).
type RoomsSource interface {
	Hotel(ctx context.Context, hotelID int64) (rooms.HotelRooms, error)
}

// DemandRecorder — запись спроса: одна сессия увеличивает спрос по ключу один раз.
type DemandRecorder interface {
	Record(ctx context.Context, regionID int64, sessionID string, keys []string) error
}

// PoiSource — ближайшие заведения для желаний «рядом с …». Реализация — adapter/repository/postgres.
type PoiSource interface {
	// NearestPoi — для каждого значения pois.kind из kinds ближайшее заведение не дальше radiusM по прямой от отеля.
	NearestPoi(ctx context.Context, hotelID int64, kinds []string, radiusM float64) (map[string]domain.NearbyPoi, error)
}

// Service — поиск. Demand == nil — спрос не пишется (демо-прогоны и тесты). Poi == nil — желания near_poi.* без данных.
type Service struct {
	Registry domain.Registry
	Store    Store
	Rooms    RoomsSource
	Demand   DemandRecorder
	Poi      PoiSource
	RegionID int64
}

// Result — выдача поиска.
type Result struct {
	Query  domain.Query // нормализованный: значения по умолчанию подставлены
	Hotels []domain.HotelResult
	// Considered — сколько отелей рассмотрено; Dropped — отброшено фильтрами карточки (звёзды, цена).
	Considered, Dropped int
	// Groups — выдача по группам: «всё сразу» и блоки компромиссов (domain.BuildGroups). Плоский Hotels сохранён.
	Groups []domain.Group
}

// Search возвращает отели с лучшим вариантом номера, упорядоченные по оценке; отели с нарушенными must — в конце.
func (s *Service) Search(ctx context.Context, q domain.Query) (Result, error) {
	q, chars, err := s.normalize(q)
	if err != nil {
		return Result{}, err
	}
	cands, err := s.Store.Candidates(ctx, s.RegionID)
	if err != nil {
		return Result{}, fmt.Errorf("кандидаты: %w", err)
	}
	type slot struct {
		res domain.HotelResult
		ok  bool
		err error
	}
	slots := make([]slot, len(cands))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, c := range cands {
		if q.Filters.StarsMin > 0 && c.Card.Stars < q.Filters.StarsMin {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res, _, ok, err := s.evalHotel(ctx, c, q, chars)
			slots[i] = slot{res, ok, err}
		}()
	}
	wg.Wait()
	out := Result{Query: q, Considered: len(cands)}
	for _, sl := range slots {
		if sl.err != nil {
			return Result{}, sl.err
		}
		if sl.ok {
			out.Hotels = append(out.Hotels, sl.res)
		}
	}
	out.Dropped = out.Considered - len(out.Hotels)
	domain.RankHotels(out.Hotels)
	out.Groups = domain.BuildGroups(out.Hotels, q.Limit)
	if len(out.Hotels) > q.Limit {
		out.Hotels = out.Hotels[:q.Limit]
	}
	if s.Demand != nil && len(q.Wishes) > 0 && !q.NoDemand {
		keys := make([]string, len(q.Wishes))
		for i, w := range q.Wishes {
			keys[i] = w.Key
		}
		if err := s.Demand.Record(ctx, s.RegionID, q.SessionID, keys); err != nil {
			return Result{}, fmt.Errorf("запись спроса: %w", err)
		}
	}
	return out, nil
}

// normalize проверяет желания по схеме реестра и подставляет значения по умолчанию.
func (s *Service) normalize(q domain.Query) (domain.Query, map[string]domain.Characteristic, error) {
	if len(q.Wishes) == 0 && q.Filters == (domain.Filters{}) {
		return q, nil, &ValidationError{"запрос пуст: нужны желания или фильтры"}
	}
	if q.Filters.PriceMax < 0 || q.Filters.StarsMin < 0 || q.Filters.StarsMin > 5 {
		return q, nil, &ValidationError{"фильтры вне допустимых границ"}
	}
	switch {
	case q.Limit <= 0:
		q.Limit = domain.DefaultSearchLimit
	case q.Limit > domain.MaxSearchLimit:
		q.Limit = domain.MaxSearchLimit
	}
	chars := map[string]domain.Characteristic{}
	ws := make([]domain.Desire, 0, len(q.Wishes))
	for _, w := range q.Wishes {
		if _, dup := chars[w.Key]; dup {
			return q, nil, &ValidationError{fmt.Sprintf("желание %q указано дважды", w.Key)}
		}
		nw, c, err := domain.NormalizeWish(s.Registry, w)
		if err != nil {
			return q, nil, &ValidationError{err.Error()}
		}
		chars[w.Key] = c
		ws = append(ws, nw)
	}
	q.Wishes = ws
	return q, chars, nil
}

// evalHotel строит лучший вариант номера отеля; ok == false — у отеля нет подходящих вариантов (фильтр цены, нет категорий).
func (s *Service) evalHotel(ctx context.Context, c Candidate, q domain.Query, chars map[string]domain.Characteristic) (domain.HotelResult, []domain.Variant, bool, error) {
	hr, err := s.Rooms.Hotel(ctx, c.ID)
	if errors.Is(err, rooms.ErrNotFound) {
		return domain.HotelResult{}, nil, false, nil
	}
	if err != nil {
		return domain.HotelResult{}, nil, false, fmt.Errorf("категории отеля %d: %w", c.ID, err)
	}
	facts, err := s.Store.HotelFacts(ctx, c.ID)
	if err != nil {
		return domain.HotelResult{}, nil, false, fmt.Errorf("факты отеля %d: %w", c.ID, err)
	}
	byKey := hotelFacts(facts)
	azimuth := map[int64]float64{}
	for _, f := range hr.Facades {
		azimuth[f.ID] = f.AzimuthDeg
	}
	wishGuaranteed, wishRefused := wishGuarantee(byKey)

	near, err := s.nearbyFor(ctx, c.ID, q.Wishes)
	if err != nil {
		return domain.HotelResult{}, nil, false, err
	}
	hotelEvals := map[string]domain.WishEval{}
	categoryKeys := map[string]bool{}
	for _, w := range q.Wishes {
		ch := chars[w.Key]
		if domain.IsNearPoiKey(w.Key) {
			hotelEvals[w.Key] = s.evalNearPoi(ch, w, near)
			continue
		}
		if domain.CategoryLevel(ch) {
			categoryKeys[w.Key] = true
			continue
		}
		var f *domain.Fact
		var src []rooms.EvidenceView
		if fv, ok := byKey[w.Key]; ok {
			f, src = &fv.Fact, fv.Evidence
		}
		e := domain.EvalHotelWish(ch, w, f)
		e.Sources = sources(src)
		hotelEvals[w.Key] = e
	}

	var variants []domain.Variant
	for _, cv := range hr.Categories {
		price := domain.CategoryPriceEUR(c.Card, cv.Category.Name)
		if q.Filters.PriceMax > 0 && price > q.Filters.PriceMax {
			continue
		}
		evals := make([]domain.WishEval, 0, len(q.Wishes))
		for _, w := range q.Wishes {
			if !categoryKeys[w.Key] {
				evals = append(evals, hotelEvals[w.Key])
				continue
			}
			target, _ := w.Value.(string)
			a, ok := hr.AssessAt(cv.Category.ID, w.Key, target, w.MinFloor)
			if !ok {
				evals = append(evals, domain.WishEval{Key: w.Key, Importance: w.Importance, Status: domain.StatusUnknown, Score: domain.UnknownScore,
					Caveat: "категория не оценена — спросим отель"})
				continue
			}
			in := domain.CategoryWishInput{Assessment: a, WishGuaranteed: wishGuaranteed, WishRefused: wishRefused,
				AzimuthOf: func(id int64) float64 { return azimuth[id] }}
			var src []rooms.EvidenceView
			for _, chv := range cv.Characteristics {
				if chv.Key == w.Key && chv.Fact != nil {
					in.Fact, src = &chv.Fact.Fact, chv.Fact.Evidence
				}
			}
			e := domain.EvalCategoryWish(chars[w.Key], w, in)
			e.Sources = sources(src)
			evals = append(evals, e)
		}
		variants = append(variants, domain.NewVariant(cv.Category.ID, cv.Category.Name, cv.Category.Rooms, price, evals, categoryKeys))
	}
	best, alt, ok := domain.ChooseVariants(variants)
	if !ok {
		return domain.HotelResult{}, nil, false, nil
	}
	return domain.HotelResult{HotelID: c.ID, Name: c.Name, District: c.District, Card: c.Card, Best: best, Alt: alt,
		OtherConflicts: domain.ConflictsOutside(variants, best.CategoryID)}, variants, true, nil
}

// nearbyFor запрашивает ближайшие заведения для желаний near_poi.*; без таких желаний (и без источника) запроса нет.
func (s *Service) nearbyFor(ctx context.Context, hotelID int64, ws []domain.Desire) (map[string]domain.NearbyPoi, error) {
	var kinds []string
	for _, w := range ws {
		if k, ok := s.Registry.Poi().ByKey(w.Key); ok {
			kinds = append(kinds, k.Kinds...)
		}
	}
	if len(kinds) == 0 || s.Poi == nil {
		return nil, nil
	}
	m, err := s.Poi.NearestPoi(ctx, hotelID, kinds, domain.PoiRadiusM)
	if err != nil {
		return nil, fmt.Errorf("заведения рядом с отелем %d: %w", hotelID, err)
	}
	return m, nil
}

// evalNearPoi оценивает желание «рядом с …»: ближайшее заведение среди значений kind типа (у группы — любого из них).
// Источника заведений нет — данных нет (спросим отель), а не «не нашли».
func (s *Service) evalNearPoi(ch domain.Characteristic, w domain.Desire, near map[string]domain.NearbyPoi) domain.WishEval {
	k, ok := s.Registry.Poi().ByKey(w.Key)
	if !ok || s.Poi == nil {
		return domain.WishEval{Key: w.Key, Importance: w.Importance, Status: domain.StatusUnknown, Score: domain.UnknownScore,
			Caveat: "заведения рядом не проверены"}
	}
	var best *domain.NearbyPoi
	for _, kind := range k.Kinds {
		if n, found := near[kind]; found && (best == nil || n.DistM < best.DistM) {
			n.Label = k.LabelOf(kind)
			best = &n
		}
	}
	return domain.EvalNearPoi(ch, w, best)
}

// hotelFacts — факты уровня отеля по ключу.
func hotelFacts(fs []FactView) map[string]FactView {
	out := map[string]FactView{}
	for _, f := range fs {
		if f.Fact.EntityType == domain.EntityHotel {
			out[f.Fact.Key] = f
		}
	}
	return out
}

// wishGuarantee — ответ отельера о пожеланиях к номеру: гарантирует (yes) или нет (refused); нет факта — оба false.
func wishGuarantee(byKey map[string]FactView) (yes, refused bool) {
	fv, ok := byKey[domain.WishGuaranteeKey]
	if !ok {
		return false, false
	}
	v, ok := fv.Fact.Value["available"].(bool)
	return ok && v, ok && !v
}

func sources(evs []rooms.EvidenceView) []domain.EvidenceSource {
	out := make([]domain.EvidenceSource, len(evs))
	for i, e := range evs {
		out[i] = domain.EvidenceSource{ID: e.ID, Source: e.Source, AuthorType: e.AuthorType, AuthorID: e.AuthorID,
			AuthorTrust: e.AuthorTrust, Reliability: e.Reliability, Contribution: e.Contribution, ObservedAt: e.ObservedAt, Claimed: e.Claimed}
	}
	return out
}

// HotelCard — карточка отеля: мок-карточка, факты с источниками, категории с вероятностями и конфликтами.
type HotelCard struct {
	Candidate
	Facts []FactView
	Rooms *rooms.HotelRooms // nil — у отеля нет категорий
}

// Hotels — отели, в которых можно искать (для разбора ссылок «похожий на …» и проверки названий в тексте).
func (s *Service) Hotels(ctx context.Context) ([]Candidate, error) {
	return s.Store.Candidates(ctx, s.RegionID)
}

// Hotel собирает карточку отеля.
func (s *Service) Hotel(ctx context.Context, id int64) (HotelCard, error) {
	c, err := s.Store.Candidate(ctx, id)
	if err != nil {
		return HotelCard{}, err
	}
	out := HotelCard{Candidate: c}
	if out.Facts, err = s.Store.HotelFacts(ctx, id); err != nil {
		return out, fmt.Errorf("факты отеля: %w", err)
	}
	hr, err := s.Rooms.Hotel(ctx, id)
	switch {
	case errors.Is(err, rooms.ErrNotFound):
	case err != nil:
		return out, fmt.Errorf("категории отеля: %w", err)
	default:
		out.Rooms = &hr
	}
	return out, nil
}

// SimilarHotel — отель, похожий на заданный, и чем они различаются.
type SimilarHotel struct {
	Candidate
	Distance float64
	// Dims — характеристики по убыванию различия (первые — главное отличие).
	Dims []domain.Dim
}

// Similar ищет отели с близким профилем фактов (без звёзд и цены). weights — веса характеристик, по умолчанию 1.
func (s *Service) Similar(ctx context.Context, id int64, limit int, weights map[string]float64) ([]SimilarHotel, error) {
	base, err := s.Store.Candidate(ctx, id)
	if err != nil {
		return nil, err
	}
	baseFacts, err := s.Store.HotelFacts(ctx, id)
	if err != nil {
		return nil, err
	}
	bp := domain.BuildProfile(s.Registry, plainFacts(baseFacts))
	if len(bp) == 0 {
		return nil, fmt.Errorf("у отеля %d нет фактов для профиля", id)
	}
	cands, err := s.Store.Candidates(ctx, s.RegionID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	var out []SimilarHotel
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	var firstErr error
	for _, c := range cands {
		if c.ID == base.ID {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fs, err := s.Store.HotelFacts(ctx, c.ID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				firstErr = err
				return
			}
			d, dims := domain.ProfileDistance(s.Registry, bp, domain.BuildProfile(s.Registry, plainFacts(fs)), weights)
			sort.SliceStable(dims, func(i, j int) bool { return dims[i].Distance*dims[i].Weight > dims[j].Distance*dims[j].Weight })
			out = append(out, SimilarHotel{Candidate: c, Distance: d, Dims: dims})
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func plainFacts(fs []FactView) []domain.Fact {
	out := make([]domain.Fact, len(fs))
	for i, f := range fs {
		out[i] = f.Fact
	}
	return out
}

// HotelVariants — отель с лучшим вариантом и всеми вариантами номера для запроса (сравнение, контракт).
type HotelVariants struct {
	Result   domain.HotelResult
	Variants []domain.Variant
}

// EvalHotel оценивает один отель по запросу. Фильтры карточки (цена, звёзды) не применяются: гость уже выбрал
// отель, цена остаётся в варианте. ErrNotFound — нет отеля или у него нет категорий.
func (s *Service) EvalHotel(ctx context.Context, hotelID int64, q domain.Query) (HotelVariants, domain.Query, error) {
	q.Filters = domain.Filters{}
	if len(q.Wishes) == 0 {
		return HotelVariants{}, q, &ValidationError{"в сессии нет желаний: сравнивать не по чему"}
	}
	q, chars, err := s.normalize(q)
	if err != nil {
		return HotelVariants{}, q, err
	}
	c, err := s.Store.Candidate(ctx, hotelID)
	if err != nil {
		return HotelVariants{}, q, err
	}
	res, variants, ok, err := s.evalHotel(ctx, c, q, chars)
	if err != nil {
		return HotelVariants{}, q, err
	}
	if !ok {
		return HotelVariants{}, q, ErrNotFound
	}
	return HotelVariants{Result: res, Variants: variants}, q, nil
}

// NearbyType — ближайшее заведение типа словаря для карты и карточки отеля.
type NearbyType struct {
	Kind domain.PoiKind
	// Found == false — в радиусе PoiRadiusM заведений этого типа в OSM нет.
	Found bool
	Poi   domain.NearbyPoi
}

// Nearby возвращает для каждого запрошенного типа (id словаря, в том числе аптека и группы) ближайшее заведение отеля.
func (s *Service) Nearby(ctx context.Context, hotelID int64, ids []string) ([]NearbyType, error) {
	if s.Poi == nil {
		return nil, &ValidationError{"источник заведений не подключён"}
	}
	var kinds []domain.PoiKind
	var raw []string
	for _, id := range ids {
		k, ok := s.Registry.Poi().Get(id)
		if !ok {
			return nil, &ValidationError{fmt.Sprintf("тип заведения %q неизвестен", id)}
		}
		kinds = append(kinds, k)
		raw = append(raw, k.Kinds...)
	}
	if _, err := s.Store.Candidate(ctx, hotelID); err != nil {
		return nil, err
	}
	near, err := s.Poi.NearestPoi(ctx, hotelID, raw, domain.PoiRadiusM)
	if err != nil {
		return nil, fmt.Errorf("заведения рядом с отелем %d: %w", hotelID, err)
	}
	out := make([]NearbyType, len(kinds))
	for i, k := range kinds {
		out[i].Kind = k
		for _, kind := range k.Kinds {
			if n, ok := near[kind]; ok && (!out[i].Found || n.DistM < out[i].Poi.DistM) {
				n.Label = k.LabelOf(kind)
				out[i].Poi, out[i].Found = n, true
			}
		}
	}
	return out, nil
}

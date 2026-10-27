// Пакет metrics считает метрики качества на размеченной выборке и по сиду: точность, калибровку уверенности, покрытие,
// долю вопросов отельерам и согласие двух источников шума. Только чтение: ни фактов, ни свидетельств, ни вопросов
// пакет не меняет. Описание метрик и их границ — docs/metrics.md.
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Уверенность разметчика.
const (
	Sure    = "sure"    // «уверен»: источник говорит прямо
	Likely  = "likely"  // «скорее»: косвенные признаки или один источник
	Unknown = "unknown" // «не знаю»: в публичных источниках ответа нет
)

// Цели разметки.
const (
	TargetSeaView     = "sea_view.has_view"        // есть номера с видом на море (порт тоже море)
	TargetQuietRooms  = "noise_night.quiet_rooms"  // есть тихие ночью номера
	TargetStreetNoisy = "noise_night.street_noisy" // со стороны улицы ночью шумно
)

// Targets — цели в порядке вывода.
var Targets = []string{TargetSeaView, TargetQuietRooms, TargetStreetNoisy}

// ConfidentFact — порог уверенного факта в покрытии (совпадает с domain.ConfidentFact).
const ConfidentFact = 0.75

// QuestionKeyWishGuarantee исключается из метрик вопросов: его создают раунды и агент как кандидата, хотя нужен он
// только из броней (docs/hotelier.md, «Известный дефект»).
const QuestionKeyWishGuarantee = "wish_guarantee"

// Mark — одна метка: значение, уверенность разметчика, ссылка на источник.
type Mark struct {
	Value      *bool
	Confidence string
	Source     string
	Note       string
}

// HotelLabel — метки одного отеля. Group: coastal (до 400 м от берега) или urban.
type HotelLabel struct {
	HotelID int64
	Name    string
	Group   string
	Marks   map[string]Mark // по целям
}

// HotelFact — итоговый факт уровня отеля.
type HotelFact struct {
	HotelID    int64
	Key        string
	Confidence float64
	Value      map[string]any
}

// QuestionRow — вопрос интервьюера отельеру.
type QuestionRow struct {
	HotelID int64
	Key     string
	Status  string
	Sent    bool // отправлен отельеру (есть sent_at)
}

// FacadeNoise — оценки шума фасада по двум источникам: карта BCN и модель по OSM. Пустой уровень — источника нет.
type FacadeNoise struct {
	FacadeID  int64
	BCNLevel  string
	OSMLevel  string
	BCNLn     *float64
	OSMLn     *float64
	DemoHotel bool
}

// CharInfo — характеристика реестра, нужная для таблиц покрытия и вопросов.
type CharInfo struct {
	Key, Title  string
	Level       string
	AskHotelier bool
}

// Input — всё, что читается из БД и файлов разметки.
type Input struct {
	DemoHotels      int
	Labels          []HotelLabel
	Facts           []HotelFact
	Questions       []QuestionRow
	Facades         []FacadeNoise
	Characteristics []CharInfo
}

// Accuracy — точность по одной цели.
type Accuracy struct {
	Target string
	// Labeled — отелей с меткой в файле; Unknown — «не знаю»; NoFact — размечены, но факта у отеля нет.
	Labeled, Unknown, NoFact int
	// Scored — метки «уверен» и «скорее» с фактом; Correct — из них совпало. Strict* — только «уверен».
	Scored, Correct             int
	StrictScored, StrictCorrect int
	// Матрица по Scored: положительный класс — «да» (вид есть, тихо, шумно).
	TP, FP, FN, TN int
	ByGroup        map[string][2]int // группа → {оценено, совпало}
}

// Bucket — корзина калибровки: точность по фактам с уверенностью из диапазона.
type Bucket struct {
	From, To        float64
	Scored, Correct int
}

// CoverageRow — покрытие характеристики уровня отеля демо-отелями.
type CoverageRow struct {
	Key, Title    string
	WithFact      int
	Confident     int
	DemoHotels    int
	AvgConfidence float64
}

// QuestionRowStat — вопросы по характеристике (без wish_guarantee).
type QuestionStat struct {
	Key, Title string
	Candidates int // всего кандидатов
	Sent       int // отправлено отельеру
	Answered   int // получен разобранный ответ
	LowValue   int // отсеяно порогом ценности
	Budget     int // ждёт недельного бюджета
	Hotels     int // отелей, у которых был кандидат
}

// Miss — размеченный отель, у которого вывод системы не совпал с меткой.
type Miss struct {
	HotelID   int64
	Name      string
	Target    string
	Label     bool
	Predicted bool
	LabelConf string
	FactConf  float64
	Source    string
}

// Agreement — согласие карты BCN и модели по OSM на фасадах, где есть оба источника.
type Agreement struct {
	Facades       int
	Exact         int
	WithinOne     int
	Matrix        [3][3]int // строки — BCN, столбцы — OSM: quiet, moderate, loud
	Pearson       float64   // по ln_db; NaN, если мало точек
	PearsonPoints int
}

// Report — результат расчёта.
type Report struct {
	DemoHotels    int
	Accuracy      []Accuracy
	Calibration   []Bucket
	Coverage      []CoverageRow
	Questions     []QuestionStat
	QuestionsAll  QuestionStat // сумма по характеристикам
	CharsAsked    int          // характеристик с вопросом отельеру (ask_hotelier), по которым отправлен хотя бы один вопрос
	CharsAskable  int          // характеристик с ask_hotelier, кроме районных и wish_guarantee
	HotelsAsked   int          // демо-отелей хотя бы с одним отправленным вопросом
	Agreement     Agreement    // демо-отели
	AgreementAll  Agreement    // все отели (сверка с этапом 3)
	Misses        []Miss
	LabeledHotels int
}

var levels = map[string]int{"quiet": 0, "moderate": 1, "loud": 2}

// Predict превращает итоговый факт отеля в ответ на цель разметки.
func Predict(target string, f HotelFact) bool {
	switch target {
	case TargetSeaView:
		return f.Value["min_floor"] != nil
	case TargetQuietRooms:
		return f.Value["level"] == "quiet"
	case TargetStreetNoisy:
		return f.Value["loudest_level"] == "loud"
	}
	return false
}

func factKey(target string) string {
	if target == TargetSeaView {
		return "sea_view"
	}
	return "noise_night"
}

var bucketEdges = []float64{0, 0.6, 0.75, 0.9, 1.0001}

// Compute считает отчёт. Чистая функция.
func Compute(in Input) Report {
	r := Report{DemoHotels: in.DemoHotels, LabeledHotels: len(in.Labels)}
	facts := map[string]HotelFact{}
	for _, f := range in.Facts {
		facts[fmt.Sprintf("%d/%s", f.HotelID, f.Key)] = f
	}

	r.Calibration = make([]Bucket, len(bucketEdges)-1)
	for i := range r.Calibration {
		r.Calibration[i] = Bucket{From: bucketEdges[i], To: bucketEdges[i+1]}
	}
	for _, t := range Targets {
		a := Accuracy{Target: t, ByGroup: map[string][2]int{}}
		for _, l := range in.Labels {
			m, ok := l.Marks[t]
			if !ok {
				continue
			}
			a.Labeled++
			if m.Confidence == Unknown || m.Value == nil {
				a.Unknown++
				continue
			}
			f, has := facts[fmt.Sprintf("%d/%s", l.HotelID, factKey(t))]
			if !has {
				a.NoFact++
				continue
			}
			pred := Predict(t, f)
			conf := math.Round(f.Confidence*100) / 100 // 0,6 из float32 не должно съезжать в соседнюю корзину
			ok = pred == *m.Value
			a.Scored++
			g := a.ByGroup[l.Group]
			g[0]++
			if ok {
				a.Correct++
				g[1]++
			}
			a.ByGroup[l.Group] = g
			if !ok {
				r.Misses = append(r.Misses, Miss{HotelID: l.HotelID, Name: l.Name, Target: t, Label: *m.Value, Predicted: pred,
					LabelConf: m.Confidence, FactConf: conf, Source: m.Source})
			}
			if m.Confidence == Sure {
				a.StrictScored++
				if ok {
					a.StrictCorrect++
				}
			}
			switch {
			case pred && *m.Value:
				a.TP++
			case pred && !*m.Value:
				a.FP++
			case !pred && *m.Value:
				a.FN++
			default:
				a.TN++
			}
			for i := range r.Calibration {
				b := &r.Calibration[i]
				if conf >= b.From && conf < b.To {
					b.Scored++
					if ok {
						b.Correct++
					}
				}
			}
		}
		r.Accuracy = append(r.Accuracy, a)
	}

	// Покрытие: характеристики с фактом уровня отеля (в том числе фасадные sea_view и noise_night, у которых есть итог
	// по отелю), по демо-отелям.
	byKey := map[string][]HotelFact{}
	for _, f := range in.Facts {
		byKey[f.Key] = append(byKey[f.Key], f)
	}
	titles := map[string]string{}
	for _, c := range in.Characteristics {
		titles[c.Key] = c.Title
		if c.Key == QuestionKeyWishGuarantee || (c.Level != "hotel" && len(byKey[c.Key]) == 0) {
			continue
		}
		row := CoverageRow{Key: c.Key, Title: c.Title, DemoHotels: in.DemoHotels}
		var sum float64
		for _, f := range byKey[c.Key] {
			row.WithFact++
			sum += f.Confidence
			if f.Confidence >= ConfidentFact {
				row.Confident++
			}
		}
		if row.WithFact > 0 {
			row.AvgConfidence = sum / float64(row.WithFact)
		}
		r.Coverage = append(r.Coverage, row)
	}

	// Вопросы (без wish_guarantee).
	stat := map[string]*QuestionStat{}
	hotelsByKey := map[string]map[int64]bool{}
	askedHotels := map[int64]bool{}
	for _, q := range in.Questions {
		if q.Key == QuestionKeyWishGuarantee {
			continue
		}
		s := stat[q.Key]
		if s == nil {
			s = &QuestionStat{Key: q.Key, Title: titles[q.Key]}
			stat[q.Key] = s
			hotelsByKey[q.Key] = map[int64]bool{}
		}
		s.Candidates++
		hotelsByKey[q.Key][q.HotelID] = true
		if q.Sent {
			s.Sent++
			askedHotels[q.HotelID] = true
		}
		switch q.Status {
		case "answered":
			s.Answered++
		case "pending_low_value":
			s.LowValue++
		case "pending":
			s.Budget++
		}
	}
	keys := make([]string, 0, len(stat))
	for k := range stat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := stat[k]
		s.Hotels = len(hotelsByKey[k])
		r.Questions = append(r.Questions, *s)
		r.QuestionsAll.Candidates += s.Candidates
		r.QuestionsAll.Sent += s.Sent
		r.QuestionsAll.Answered += s.Answered
		r.QuestionsAll.LowValue += s.LowValue
		r.QuestionsAll.Budget += s.Budget
	}
	for _, c := range in.Characteristics {
		if c.Level == "district" || !c.AskHotelier || c.Key == QuestionKeyWishGuarantee {
			continue
		}
		r.CharsAskable++
		if s := stat[c.Key]; s != nil && s.Sent > 0 {
			r.CharsAsked++
		}
	}
	r.HotelsAsked = len(askedHotels)

	// Согласие BCN и OSM.
	r.Agreement = agreement(in.Facades, true)
	r.AgreementAll = agreement(in.Facades, false)
	return r
}

func agreement(facades []FacadeNoise, demoOnly bool) Agreement {
	var ag Agreement
	var xs, ys []float64
	for _, f := range facades {
		b, bok := levels[f.BCNLevel]
		o, ook := levels[f.OSMLevel]
		if !bok || !ook || (demoOnly && !f.DemoHotel) {
			continue
		}
		ag.Facades++
		ag.Matrix[b][o]++
		if b == o {
			ag.Exact++
		}
		if abs(b-o) <= 1 {
			ag.WithinOne++
		}
		if f.BCNLn != nil && f.OSMLn != nil {
			xs = append(xs, *f.BCNLn)
			ys = append(ys, *f.OSMLn)
		}
	}
	ag.PearsonPoints = len(xs)
	ag.Pearson = pearson(xs, ys)
	return ag
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func pearson(xs, ys []float64) float64 {
	n := float64(len(xs))
	if n < 3 {
		return math.NaN()
	}
	var sx, sy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
	}
	mx, my := sx/n, sy/n
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	return sxy / math.Sqrt(sxx*syy)
}

func pct(n, d int) string {
	if d == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f %%", 100*float64(n)/float64(d))
}

// Render печатает отчёт таблицами Markdown (stdout и вставка в docs/metrics.md).
func Render(r Report) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("### Точность на размеченной выборке\n\n")
	w("Размечено отелей: %d. В точность идут метки «уверен» и «скорее» (колонка «строго» — только «уверен»); «не знаю» не оцениваются, но считаются.\n\n", r.LabeledHotels)
	w("| Цель | Размечено | Не знаю | Оценено | Совпало | Точность | Строго (уверен) | TP / FP / FN / TN |\n|---|---|---|---|---|---|---|---|\n")
	for _, a := range r.Accuracy {
		w("| `%s` | %d | %d | %d | %d | %s | %s (%d из %d) | %d / %d / %d / %d |\n", a.Target, a.Labeled, a.Unknown, a.Scored, a.Correct,
			pct(a.Correct, a.Scored), pct(a.StrictCorrect, a.StrictScored), a.StrictCorrect, a.StrictScored, a.TP, a.FP, a.FN, a.TN)
	}
	w("\nПо группам отелей (оценено / совпало):\n\n| Цель | Береговые | Городские |\n|---|---|---|\n")
	for _, a := range r.Accuracy {
		c, u := a.ByGroup["coastal"], a.ByGroup["urban"]
		w("| `%s` | %d / %d | %d / %d |\n", a.Target, c[0], c[1], u[0], u[1])
	}

	w("\n### Калибровка уверенности\n\n")
	w("Точность по корзинам уверенности итогового факта; в корзину попадают все оценённые метки всех целей. Выборка мала — это иллюстрация, а не оценка.\n\n")
	w("| Уверенность факта | Оценено | Совпало | Точность |\n|---|---|---|---|\n")
	for _, bk := range r.Calibration {
		rng := fmt.Sprintf("[%s; %s)", comma(bk.From), comma(bk.To))
		if bk.To > 1 {
			rng = fmt.Sprintf("[%s; 1,00]", comma(bk.From))
		}
		w("| %s | %d | %d | %s |\n", rng, bk.Scored, bk.Correct, pct(bk.Correct, bk.Scored))
	}

	w("\nРасхождения с разметкой (метка «уверен» или «скорее» против вывода системы):\n\n")
	if len(r.Misses) == 0 {
		w("Нет.\n")
	} else {
		w("| Отель | Цель | Метка | Система | Уверенность метки | Уверенность факта | Источник метки |\n|---|---|---|---|---|---|---|\n")
		for _, m := range r.Misses {
			w("| %d %s | `%s` | %s | %s | %s | %s | %s |\n", m.HotelID, m.Name, m.Target, yesNo(m.Label), yesNo(m.Predicted), labelConf(m.LabelConf), comma(m.FactConf), m.Source)
		}
	}

	w("\n### Покрытие демо-отелей (%d) фактами уровня отеля\n\n", r.DemoHotels)
	w("| Характеристика | С фактом | С уверенным (≥ %s) | Средняя уверенность |\n|---|---|---|---|\n", comma(ConfidentFact))
	for _, c := range r.Coverage {
		avg := "—"
		if c.WithFact > 0 {
			avg = comma(c.AvgConfidence)
		}
		w("| `%s` | %d (%s) | %d (%s) | %s |\n", c.Key, c.WithFact, pct(c.WithFact, c.DemoHotels), c.Confident, pct(c.Confident, c.DemoHotels), avg)
	}

	w("\n### Вопросы отельерам (без `wish_guarantee`)\n\n")
	w("| Характеристика | Кандидатов | Отправлено | С разобранным ответом | Отсеяно ценностью | Ждёт бюджета | Отелей |\n|---|---|---|---|---|---|---|\n")
	for _, q := range r.Questions {
		w("| `%s` | %d | %d | %d | %d | %d | %d |\n", q.Key, q.Candidates, q.Sent, q.Answered, q.LowValue, q.Budget, q.Hotels)
	}
	all := r.QuestionsAll
	w("| **Всего** | %d | %d | %d | %d | %d | |\n\n", all.Candidates, all.Sent, all.Answered, all.LowValue, all.Budget)
	w("- Характеристик с вопросом отельеру в реестре (без районных и `wish_guarantee`): %d; по ним отправлен хотя бы один вопрос: %d (%s).\n", r.CharsAskable, r.CharsAsked, pct(r.CharsAsked, r.CharsAskable))
	w("- Демо-отелей, которым отправлен хотя бы один вопрос: %d из %d (%s).\n", r.HotelsAsked, r.DemoHotels, pct(r.HotelsAsked, r.DemoHotels))
	w("- Доля кандидатов, отсеянных ценностью и бюджетом: %s (ценностью %d, бюджетом %d из %d).\n", pct(all.LowValue+all.Budget, all.Candidates), all.LowValue, all.Budget, all.Candidates)

	w("\n### Согласие карты шума BCN и оценки по OSM (фасады с двумя источниками)\n\n")
	w("Оценка по OSM подстроена под BCN (этап 3, константы подобраны один раз), поэтому согласие завышено и не доказывает точность ни одного из источников.\n\n")
	w("| Набор | Фасадов | Совпал уровень | Не дальше ступени | Корреляция Ln |\n|---|---|---|---|---|\n")
	for _, row := range []struct {
		name string
		ag   Agreement
	}{{"демо-отели", r.Agreement}, {"все отели", r.AgreementAll}} {
		corr := "—"
		if !math.IsNaN(row.ag.Pearson) {
			corr = comma(row.ag.Pearson)
		}
		w("| %s | %d | %s (%d) | %s (%d) | %s |\n", row.name, row.ag.Facades, pct(row.ag.Exact, row.ag.Facades), row.ag.Exact,
			pct(row.ag.WithinOne, row.ag.Facades), row.ag.WithinOne, corr)
	}
	w("\nМатрица на демо-отелях (строки — BCN, столбцы — OSM):\n\n| BCN \\ OSM | quiet | moderate | loud |\n|---|---|---|---|\n")
	for i, name := range []string{"quiet", "moderate", "loud"} {
		w("| %s | %d | %d | %d |\n", name, r.Agreement.Matrix[i][0], r.Agreement.Matrix[i][1], r.Agreement.Matrix[i][2])
	}
	return b.String()
}

func comma(v float64) string { return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1) }

func yesNo(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

func labelConf(c string) string {
	switch c {
	case Sure:
		return "уверен"
	case Likely:
		return "скорее"
	}
	return "не знаю"
}

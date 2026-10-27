package domain

// Гости «Гуру»: анкета после проживания по ожиданиям брони, контрольный вопрос с известным ответом, небрежность,
// репутация гостя и сигнал пересчёта. Чистая логика без БД; описание — docs/guests.md.

// Персоны имитированных гостей (guests.persona).
const (
	GuestHonest     = "honest"      // отвечает по тому, что видел: на этаже и фасаде брони это значение расчёта
	GuestLazy       = "lazy"        // отвечает «да» на всё и быстро
	GuestClosedView = "closed_view" // сообщает о закрытом виде там, где расчёт вид даёт (деревья, леса, стройка)
)

// Параметры анкеты и сигнала.
const (
	// SourceGuest — источник свидетельства гостя (evidence.source).
	SourceGuest = "guest_survey"
	// GuestBaseReliability — база надёжности свидетельства гостя (раздел «Источники и логика правды»).
	GuestBaseReliability = 0.7
	// MinSurveySeconds — анкета быстрее считается небрежной («за пару секунд»).
	MinSurveySeconds = 8
	// MinAnswersForUniform — одинаковые ответы считаются небрежностью, только если ответов не меньше.
	MinAnswersForUniform = 3
	// ControlMargin — запас контрольного вопроса: порог минимум вдвое выше (ответ «да») или вдвое ниже (ответ «нет»)
	// значения, которое система знает. Тогда ответ однозначен: погрешность расчёта (граф OSM, округление) его не меняет.
	ControlMargin = 2
	// ControlMinConfidence — контрольный вопрос строится только по факту с уверенностью не ниже этой.
	ControlMinConfidence = 0.6
	// GuruPointsPerAnswer — баллы «Гуру» за ответ: одинаковые за «да» и «нет», 0 за небрежную анкету.
	GuruPointsPerAnswer = 10
	// SignalMinGuests и SignalMinTrust — сигнал пересчёта: столько надёжных гостей независимо против расчёта.
	SignalMinGuests = 3
	SignalMinTrust  = 0.5
)

// Причины небрежности анкеты (guest_surveys.careless).
const (
	CarelessFast    = "анкета заполнена слишком быстро"
	CarelessUniform = "все ответы одинаковые"
	CarelessControl = "контрольный вопрос провален"
)

// controlThresholds — пороги контрольного вопроса в минутах пешком.
var controlThresholds = []int{2, 3, 5, 10, 15, 20, 30, 45, 60}

// ControlQuestion — контрольный вопрос «<цель> не дальше Threshold минут пешком?» с известным системе ответом.
type ControlQuestion struct {
	Key       string // характеристика (walk_to_metro, walk_to_beach)
	Threshold int
	Minutes   int  // значение факта, по которому построен вопрос
	Answer    bool // правильный ответ
}

// MakeControl строит контрольный вопрос по факту в minutes минут. wantYes выбирает правильный ответ: «да»
// (порог — наименьший, не ниже minutes × ControlMargin) или «нет» (порог — наибольший, не выше minutes / ControlMargin,
// не меньше 2 минут). false — запаса нет: вопрос был бы спорным, его не задаём.
func MakeControl(key string, minutes int, wantYes bool) (ControlQuestion, bool) {
	if minutes <= 0 {
		return ControlQuestion{}, false
	}
	q := ControlQuestion{Key: key, Minutes: minutes, Answer: wantYes}
	found := false
	if wantYes {
		for _, t := range controlThresholds {
			if t >= minutes*ControlMargin {
				q.Threshold, found = t, true
				break
			}
		}
	} else {
		for _, t := range controlThresholds {
			if t*ControlMargin <= minutes {
				q.Threshold, found = t, true
			}
		}
	}
	return q, found
}

// Careless возвращает причины небрежности анкеты: заполнена за пару секунд, все ответы одинаковые
// (answers — ответы в виде «yes» / «no» по всем вопросам, включая контрольный), контрольный вопрос провален.
// Несогласие с расчётом небрежностью не считается: оно не наказывается.
func Careless(durationSec int, answers []string, controlOK bool) []string {
	var out []string
	if durationSec < MinSurveySeconds {
		out = append(out, CarelessFast)
	}
	if len(answers) >= MinAnswersForUniform {
		same := true
		for _, a := range answers[1:] {
			if a != answers[0] {
				same = false
			}
		}
		if same {
			out = append(out, CarelessUniform)
		}
	}
	if !controlOK {
		out = append(out, CarelessControl)
	}
	return out
}

// GuruPoints — баллы за анкету: GuruPointsPerAnswer за каждый ответ, небрежная анкета баллов не даёт.
func GuruPoints(answers int, careless []string) int {
	if len(careless) > 0 {
		return 0
	}
	return answers * GuruPointsPerAnswer
}

// GuestTrust — репутация гостя: (добросовестных анкет + 1) / (анкет + 2). Добросовестная — контрольный вопрос
// пройден и небрежности нет; расхождение ответов с расчётом на репутацию не влияет.
func GuestTrust(reliable, surveys int) float64 { return TrustFromCounts(reliable, surveys) }

// SignalVote — ответ гостя: бронь, репутация, ответ и значение расчёта в той же ячейке «фасад × этаж» брони.
type SignalVote struct {
	BookingID, GuestID int64
	Trust              float64
	Value, CalcValue   string
}

// SignalVotes отбирает голоса для сигнала: гость с репутацией не ниже SignalMinTrust, ответ против расчёта
// в своей ячейке, одна бронь — один голос.
func SignalVotes(votes []SignalVote) []SignalVote {
	seen := map[int64]bool{}
	var out []SignalVote
	for _, v := range votes {
		if v.Trust < SignalMinTrust || v.Value == v.CalcValue || seen[v.BookingID] {
			continue
		}
		seen[v.BookingID] = true
		out = append(out, v)
	}
	return out
}

// SignalReached — набралось SignalMinGuests надёжных гостей против расчёта.
func SignalReached(votes []SignalVote) bool { return len(SignalVotes(votes)) >= SignalMinGuests }

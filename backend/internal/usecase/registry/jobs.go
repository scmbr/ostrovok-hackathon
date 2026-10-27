package registry

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Состояния фонового задания.
const (
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

// JobView — состояние задания для опроса из интерфейса.
type JobView struct {
	ID      string
	Title   string
	Status  string
	Done    int
	Total   int
	Message string
	// Errors — что не удалось (например, нет исходных данных у инструмента).
	Errors     []string
	StartedAt  time.Time
	FinishedAt *time.Time
}

// Progress — то, чем тело задания сообщает ход работы.
type Progress struct {
	j *Jobs
	v *JobView
}

// Report обновляет счётчик и сообщение.
func (p *Progress) Report(done, total int, msg string) {
	p.j.mu.Lock()
	defer p.j.mu.Unlock()
	p.v.Done, p.v.Total, p.v.Message = done, total, msg
}

// Fail дописывает замечание (не более 20), не останавливая задание.
func (p *Progress) Fail(msg string) {
	p.j.mu.Lock()
	defer p.j.mu.Unlock()
	if len(p.v.Errors) < 20 {
		p.v.Errors = append(p.v.Errors, msg)
	}
}

// JobFunc — тело задания.
type JobFunc func(ctx context.Context, p *Progress) error

// Jobs — фоновые задания (расчёт новой характеристики по демо-отелям). Состояние в памяти: прогресс нужен только
// пока идёт расчёт, результат лежит в фактах.
type Jobs struct {
	mu   sync.Mutex
	seq  int
	jobs map[string]*JobView
	Now  func() time.Time
}

func (j *Jobs) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// Start запускает run в фоне и возвращает id задания. Контекст запроса для фона не годится: задание живёт дольше него.
func (j *Jobs) Start(title string, run JobFunc) string {
	j.mu.Lock()
	if j.jobs == nil {
		j.jobs = map[string]*JobView{}
	}
	j.seq++
	id := fmt.Sprintf("job-%d", j.seq)
	v := &JobView{ID: id, Title: title, Status: JobRunning, StartedAt: j.now()}
	j.jobs[id] = v
	j.mu.Unlock()

	go func() {
		err := run(context.Background(), &Progress{j: j, v: v})
		j.mu.Lock()
		defer j.mu.Unlock()
		t := j.now()
		v.FinishedAt = &t
		if err != nil {
			v.Status, v.Errors = JobFailed, append(v.Errors, err.Error())
			return
		}
		v.Status = JobDone
	}()
	return id
}

// Get возвращает копию состояния задания.
func (j *Jobs) Get(id string) (JobView, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	v, ok := j.jobs[id]
	if !ok {
		return JobView{}, false
	}
	cp := *v
	cp.Errors = append([]string(nil), v.Errors...)
	return cp, true
}

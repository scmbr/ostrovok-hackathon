package http

import "net/http"

// Режимы LLM, о которых сообщает /api/meta: ключ задан — живая модель, нет — сохранённые ответы.
const (
	LLMModeLive  = "llm"
	LLMModeCache = "cache"
)

// WithLLMMode сообщает клиенту режим LLM (GET /api/meta); model показывается только в режиме llm.
func WithLLMMode(mode, model string) Option {
	return func(d *routerDeps) { d.llmMode, d.llmModel = mode, model }
}

func handleMeta(mode, model string) http.HandlerFunc {
	if mode == "" {
		mode = LLMModeCache
	}
	body := map[string]string{"llm_mode": mode}
	if mode == LLMModeLive && model != "" {
		body["llm_model"] = model
	}
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, body) }
}

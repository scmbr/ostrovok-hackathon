package llm

import "embed"

//go:embed all:cache/dialog
var dialogCacheFS embed.FS

// DialogCacheDir — куда Recorder сохраняет ответы записанных диалогов (относительно backend/).
const DialogCacheDir = "internal/adapter/llm/cache/dialog"

// NewDialogReplay читает встроенный кеш агента диалога (cache/dialog/*.json): ответ модели ищется по хешу
// сообщений шага, так что воспроизведение работает без ключа. На промахе агент диалога переходит на правила.
func NewDialogReplay() (*Replay, error) { return newReplay(dialogCacheFS, "cache/dialog") }

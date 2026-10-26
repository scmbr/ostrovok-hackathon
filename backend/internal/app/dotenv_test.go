package app

import (
	"os"
	"testing"
)

func TestLoadDotEnvKeepsEmptyEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/.env", []byte("LLM_API_KEY=secret\nDOTENV_ONLY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	t.Setenv("LLM_API_KEY", "") // пустое значение в окружении — осознанный режим кеша
	os.Unsetenv("DOTENV_ONLY")
	t.Cleanup(func() { os.Unsetenv("DOTENV_ONLY") })

	loadDotEnv()

	if got := os.Getenv("LLM_API_KEY"); got != "" {
		t.Errorf("пустая переменная окружения перезаписана из .env: %q", got)
	}
	if got := os.Getenv("DOTENV_ONLY"); got != "1" {
		t.Errorf("незаданная переменная должна браться из .env: %q", got)
	}
}

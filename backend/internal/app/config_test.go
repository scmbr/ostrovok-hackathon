package app

import "testing"

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	cfg := LoadConfig()
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.DatabaseURL == "" {
		t.Error("DatabaseURL пуст")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("LLM_API_KEY", "k")
	cfg := LoadConfig()
	if cfg.Port != "9090" || cfg.LLMAPIKey != "k" {
		t.Errorf("неожиданный конфиг: %+v", cfg)
	}
}

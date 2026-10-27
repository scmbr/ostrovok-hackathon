package app

import "os"

// Config — настройки приложения из переменных окружения.
type Config struct {
	Port        string
	DatabaseURL string
	LLMBaseURL  string
	LLMAPIKey   string
	LLMModel    string
	// RawDataDir — куда cmd/collect сохраняет сырые выгрузки (запуск из backend/).
	RawDataDir string
	// SeedFile — демо-данные (pg_dump, gzip); заливаются, если БД пуста.
	SeedFile string
}

func LoadConfig() Config {
	return Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://tima:tima@localhost:5432/tima?sslmode=disable"),
		LLMBaseURL:  getenv("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMModel:    getenv("LLM_MODEL", "gpt-4o-mini"),
		RawDataDir:  getenv("RAW_DATA_DIR", "../data/raw"),
		SeedFile:    getenv("SEED_FILE", "../data/demo/seed.sql.gz"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

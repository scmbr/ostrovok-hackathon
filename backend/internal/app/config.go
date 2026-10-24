package app

import "os"

// Config — настройки приложения из переменных окружения.
type Config struct {
	Port        string
	DatabaseURL string
	LLMBaseURL  string
	LLMAPIKey   string
	LLMModel    string
}

func LoadConfig() Config {
	return Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://tima:tima@localhost:5432/tima?sslmode=disable"),
		LLMBaseURL:  os.Getenv("LLM_BASE_URL"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMModel:    os.Getenv("LLM_MODEL"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

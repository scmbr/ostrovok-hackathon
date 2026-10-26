package app

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv подхватывает локальный .env (из текущей папки или корня репозитория) для запуска вне Docker.
// Уже заданные переменные окружения не перезаписываются; значения не выводятся.
func loadDotEnv() {
	for _, path := range []string{".env", "../.env"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			if !ok || k == "" || os.Getenv(k) != "" {
				continue
			}
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			os.Setenv(k, v)
		}
		f.Close()
		return
	}
}

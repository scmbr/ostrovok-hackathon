// Команда collect — CLI сбора и расчёта данных.
package main

import (
	"fmt"
	"os"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/app"
)

func main() {
	if err := app.RunCollect(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

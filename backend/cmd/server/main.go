package main

import (
	"log"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

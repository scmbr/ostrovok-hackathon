.PHONY: up down test

up:
	docker compose up --build

down:
	docker compose down

test:
	cd backend && go test ./... && go vet ./...

.PHONY: up down test seed

up:
	docker compose up --build

down:
	docker compose down

test:
	cd backend && go test ./... && go vet ./...

# Пересобрать демо-данные из запущенной БД (docker compose up -d db)
seed:
	docker compose exec -T db pg_dump -U tima -d tima --data-only -t hotels -t buildings -t sea -t room_categories -t roads -t facades | gzip -n > data/demo/seed.sql.gz

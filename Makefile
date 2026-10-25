.PHONY: up down test seed

up:
	docker compose up --build

down:
	docker compose down

test:
	cd backend && go test ./... && go vet ./...

# Пересобрать демо-данные из запущенной БД (docker compose up -d db).
# Перед дампом удаляются superseded-факты и свидетельства: история пересчётов нужна только в рабочей БД, демо хватает активных.
seed:
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM facts WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM evidence WHERE status = 'superseded'"
	docker compose exec -T db pg_dump -U tima -d tima --data-only -t hotels -t buildings -t sea -t room_categories -t roads -t facades -t evidence -t facts | gzip -n > data/demo/seed.sql.gz

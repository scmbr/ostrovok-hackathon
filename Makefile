.PHONY: up down test seed

up:
	docker compose up --build

down:
	docker compose down

test:
	cd backend && go test ./... && go vet ./...

# Пересобрать демо-данные из запущенной БД (docker compose up -d db). В сид входит выборка building_parts (scripts/seed_parts.sql).
# Перед дампом удаляются superseded-факты и свидетельства: история пересчётов нужна только в рабочей БД, демо хватает активных.
seed:
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM facts WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM evidence WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM conflicts WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -v ON_ERROR_STOP=1 < scripts/seed_parts.sql
	( docker compose exec -T db pg_dump -U tima -d tima --data-only -t hotels -t buildings -t seed_building_parts -t sea -t room_categories -t room_placements -t roads -t facades -t evidence -t noise_facades -t pois -t conflicts -t facts -t agent_trace -t hotelier_questions 	  | sed 's/^COPY public\.seed_building_parts /COPY public.building_parts /'; 	  echo "SELECT pg_catalog.setval('public.building_parts_id_seq', (SELECT COALESCE(max(id), 1) FROM public.building_parts), true);" ) | gzip -n > data/demo/seed.sql.gz
	docker compose exec -T db psql -U tima -d tima -c "DROP TABLE seed_building_parts"

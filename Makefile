.PHONY: up down test test-replay test-replay-dialog seed demo-reset

# Для разработчика: с портом БД 5432 на хосте (docker-compose.dev.yml). Для жюри — просто docker compose up --build.
DEV := -f docker-compose.yml -f docker-compose.dev.yml

up:
	docker compose $(DEV) up --build

down:
	docker compose $(DEV) down

test:
	cd backend && go test ./... && go vet ./...

# Реплей кеша агента на базе, поднятой из демо-сида: проект Compose clean-check (рабочая БД не трогается).
# Рабочий стек на время проверки останавливается (порты 8080 и 5432), потом поднимается снова.
# Тест идёт по пяти демо-отелям (TestAgentReplayFromSeedHasNoMisses); промах кеша — провал.
test-replay:
	docker compose stop
	status=0; \
	docker compose -p clean-check $(DEV) up --build -d --wait && \
	( cd backend && AGENT_REPLAY_DATABASE_URL="postgres://tima:tima@localhost:5432/tima?sslmode=disable" \
	  go test ./internal/app -run TestAgentReplayFromSeedHasNoMisses -count=1 -v ) || status=$$?; \
	docker compose -p clean-check down -v; \
	docker compose start; \
	exit $$status

# Реплей демо-диалогов (backend/internal/usecase/dialog/demo_dialogs.json) из кеша без ключа: 0 промахов по всем ходам.
# Тот же проект clean-check, что у test-replay; рабочий стек на время проверки останавливается.
test-replay-dialog:
	docker compose stop
	status=0; \
	docker compose -p clean-check $(DEV) up --build -d --wait && \
	( cd backend && AGENT_REPLAY_DATABASE_URL="postgres://tima:tima@localhost:5432/tima?sslmode=disable" \
	  go test ./internal/app -run TestDialogReplayFromSeedHasNoMisses -count=1 -v ) || status=$$?; \
	docker compose -p clean-check down -v; \
	docker compose start; \
	exit $$status

# Пересобрать демо-данные из запущенной БД (docker compose up -d db). В сид входит выборка building_parts (scripts/seed_parts.sql).
# Перед дампом удаляются спрос из поиска (в сид идёт только мок спроса), журнал диалогов гостей, брони из диалога (session_id), superseded-факты и свидетельства: история пересчётов нужна только в рабочей БД, демо хватает активных.
seed:
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM facts WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM evidence WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM conflicts WHERE status = 'superseded'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM characteristic_demand WHERE source = 'search'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM agent_trace WHERE agent = 'guest_dialog'"
	docker compose exec -T db psql -U tima -d tima -c "DELETE FROM bookings WHERE session_id IS NOT NULL"
	docker compose exec -T db psql -U tima -d tima -v ON_ERROR_STOP=1 < scripts/seed_parts.sql
	( docker compose exec -T db pg_dump -U tima -d tima --data-only -t hotels -t buildings -t seed_building_parts -t sea -t room_categories -t room_placements -t roads -t facades -t evidence -t noise_facades -t pois -t conflicts -t facts -t agent_trace -t hotelier_questions -t characteristic_demand -t mock_hotel_truth -t author_trust -t guests -t bookings -t guest_surveys -t recompute_tasks 	  | sed 's/^COPY public\.seed_building_parts /COPY public.building_parts /'; 	  echo "SELECT pg_catalog.setval('public.building_parts_id_seq', (SELECT COALESCE(max(id), 1) FROM public.building_parts), true);" ) | gzip -n > data/demo/seed.sql.gz
	docker compose exec -T db psql -U tima -d tima -c "DROP TABLE seed_building_parts"

# Вернуть постоянный стенд demo (localhost:8081) к исходному состоянию: сид заливается заново.
# Сценарии кабинета отельера и «под капотом» меняют данные; перед записью видео стенд сбрасывается.
# Проект demo отдельный: рабочая БД и проекты по умолчанию и clean-check не затрагиваются.
demo-reset:
	docker compose -p demo -f docker-compose.yml -f docker-compose.demo.yml down -v
	docker compose -p demo -f docker-compose.yml -f docker-compose.demo.yml up --build -d --wait

-- Этап 5.3: гости «Гуру». Имитированные брони, анкеты после проживания, задачи пересчёта по сигналу гостей.

-- Гость-персона мока: honest | lazy | closed_view. guru_points — баллы за честные ответы (одинаковые за «да» и «нет»).
CREATE TABLE guests (
    id          bigserial PRIMARY KEY,
    name        text   NOT NULL,
    persona     text   NOT NULL CHECK (persona IN ('honest', 'lazy', 'closed_view')),
    guru_points int    NOT NULL DEFAULT 0,
    profile     jsonb  NOT NULL DEFAULT '{}',
    consent     jsonb  NOT NULL DEFAULT '{}'
);

-- Бронь с пожеланием: категория, фасад и этаж, где гостя поселили (из мока заселения).
ALTER TABLE bookings
    ADD COLUMN guest_id  bigint REFERENCES guests (id) ON DELETE CASCADE,
    ADD COLUMN facade_id bigint REFERENCES facades (id) ON DELETE SET NULL,
    ADD COLUMN floor     int;
CREATE INDEX bookings_guest_idx ON bookings (guest_id);

-- Анкета после проживания. questions и answers — списки по порядку; control_ok — контрольный вопрос;
-- careless — причины небрежности (пусто — анкета заполнена добросовестно); scope — какие ответы вошли в свидетельства
-- (ответ с этажа, где расчёт даёт другое значение, чем у категории в целом, категорию не опровергает).
CREATE TABLE guest_surveys (
    id           bigserial PRIMARY KEY,
    guest_id     bigint  NOT NULL REFERENCES guests (id) ON DELETE CASCADE,
    booking_id   bigint  NOT NULL UNIQUE REFERENCES bookings (id) ON DELETE CASCADE,
    questions    jsonb   NOT NULL,
    answers      jsonb   NOT NULL,
    control_ok   boolean NOT NULL,
    duration_sec int     NOT NULL,
    careless     jsonb   NOT NULL DEFAULT '[]',
    scope        jsonb   NOT NULL DEFAULT '[]',
    points       int     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Задача пересчёта: несколько надёжных гостей независимо против расчёта, факт помечен stale.
-- outcome: confirmed (пересчёт подтвердил расчёт) | changed (значение изменилось).
CREATE TABLE recompute_tasks (
    id                 bigserial PRIMARY KEY,
    hotel_id           bigint NOT NULL REFERENCES hotels (id) ON DELETE CASCADE,
    characteristic_key text   NOT NULL,
    reason             text   NOT NULL,
    signal             jsonb  NOT NULL DEFAULT '{}',
    status             text   NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done')),
    outcome            text,
    result             jsonb  NOT NULL DEFAULT '{}',
    created_at         timestamptz NOT NULL DEFAULT now(),
    done_at            timestamptz
);
CREATE UNIQUE INDEX recompute_tasks_open_uq ON recompute_tasks (hotel_id, characteristic_key) WHERE status = 'open';

-- Журнал решений по правилу (без модели): сигнал гостей и пересчёт.
ALTER TABLE agent_trace DROP CONSTRAINT agent_trace_mode_check;
ALTER TABLE agent_trace ADD CONSTRAINT agent_trace_mode_check CHECK (mode IN ('llm', 'cache', 'planner', 'rule'));

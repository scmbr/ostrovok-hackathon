-- Журнал решений агентов (раздел «Новые таблицы» PROJECT_CONTEXT.md) для «под капотом».
-- mode: llm (живая модель), cache (ответы модели из кеша), planner (детерминированный планировщик).
CREATE TABLE agent_trace (
    id         bigserial PRIMARY KEY,
    agent      text NOT NULL,
    session_id text NOT NULL,
    hotel_id   bigint REFERENCES hotels (id) ON DELETE CASCADE,
    mode       text NOT NULL CHECK (mode IN ('llm', 'cache', 'planner')),
    step       int NOT NULL,
    decision   text NOT NULL,
    tool       text,
    input      jsonb NOT NULL DEFAULT '{}',
    output     jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_trace_hotel_idx ON agent_trace (hotel_id, session_id, step);

-- Один открытый вопрос на характеристику отеля: агент не плодит дубли между прогонами.
CREATE UNIQUE INDEX hotelier_questions_open_uq ON hotelier_questions (hotel_id, characteristic_key) WHERE status = 'open';

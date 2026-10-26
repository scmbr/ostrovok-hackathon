-- Этап 6.1: спрос из поиска. Мок спроса (source = 'mock') и спрос поиска (source = 'search') — отдельные строки.
ALTER TABLE characteristic_demand DROP CONSTRAINT characteristic_demand_pkey;
ALTER TABLE characteristic_demand ADD PRIMARY KEY (characteristic_key, region_id, source);

-- Одна сессия поиска увеличивает спрос по ключу один раз, сколько бы запросов она ни сделала.
CREATE TABLE demand_sessions (
    session_id         text        NOT NULL,
    characteristic_key text        NOT NULL,
    region_id          bigint      NOT NULL REFERENCES regions (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, characteristic_key, region_id)
);

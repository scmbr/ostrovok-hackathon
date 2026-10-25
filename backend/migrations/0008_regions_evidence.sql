-- Регион: охват данных. Все исходные таблицы принадлежат региону; существующие строки — Барселона.
CREATE TABLE regions (
    id   bigserial PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL
);
INSERT INTO regions (id, code, name) VALUES (1, 'barcelona', 'Барселона');
SELECT setval(pg_get_serial_sequence('regions', 'id'), 1);

ALTER TABLE hotels         ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
ALTER TABLE buildings      ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
ALTER TABLE pois           ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
ALTER TABLE noise_segments ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
ALTER TABLE sea            ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
ALTER TABLE walk_edges     ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
ALTER TABLE roads          ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);
CREATE INDEX hotels_region_idx ON hotels (region_id);
CREATE INDEX buildings_region_idx ON buildings (region_id);

-- Свидетельства: каждое утверждение об (сущность, характеристика) от источника или автора.
-- Факты в facts — итог сложения свидетельств, ссылаются на них через evidence_ids.
CREATE TABLE evidence (
    id                 bigserial PRIMARY KEY,
    entity_type        text NOT NULL,
    entity_id          text NOT NULL,
    characteristic_key text NOT NULL,
    claimed_value      jsonb NOT NULL,
    source             text NOT NULL,   -- osm | catastro | bcn_noise | open_meteo | computed | hotelier | guest
    method             text NOT NULL DEFAULT '',  -- инструмент и версия алгоритма (для расчёта)
    author_type        text NOT NULL,   -- tool | hotelier | guest
    author_id          text NOT NULL,
    reliability        real NOT NULL CHECK (reliability > 0 AND reliability < 1),
    contribution       real NOT NULL,   -- вклад в логарифм шансов
    observed_at        timestamptz NOT NULL DEFAULT now(),
    valid_until        timestamptz,
    details            jsonb NOT NULL DEFAULT '{}',  -- параметры расчёта, текст ответа, этаж гостя
    status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded'))
);
CREATE INDEX evidence_active_idx ON evidence (entity_type, entity_id, characteristic_key) WHERE status = 'active';

CREATE TABLE author_trust (
    author_type text NOT NULL,
    author_id   text NOT NULL,
    agree       int NOT NULL DEFAULT 0,
    total       int NOT NULL DEFAULT 0,
    trust       real NOT NULL DEFAULT 0.5,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (author_type, author_id)
);

ALTER TABLE facts ADD COLUMN evidence_ids bigint[] NOT NULL DEFAULT '{}';

-- Вопросы отельеру: ценность вопроса и персона имитированного отельера.
ALTER TABLE hotelier_questions ADD COLUMN value_score real;
ALTER TABLE hotelier_questions ADD COLUMN persona text;

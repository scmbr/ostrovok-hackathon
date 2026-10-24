CREATE EXTENSION IF NOT EXISTS postgis;

-- Исходные геоданные (заполняет cmd/collect, попадают в сиды)
CREATE TABLE buildings (
    id            bigserial PRIMARY KEY,
    osm_id        bigint,
    catastro_ref  text,
    geom          geometry(MultiPolygon, 4326) NOT NULL,
    levels        int,
    height_m      real,
    height_source text NOT NULL DEFAULT 'default'  -- catastro | osm_levels | osm_height | default
);
CREATE INDEX buildings_geom_idx ON buildings USING gist (geom);

CREATE TABLE noise_segments (
    id         bigserial PRIMARY KEY,
    source_ref text,
    geom       geometry(LineString, 4326) NOT NULL,
    lden_db    real,
    ln_db      real
);
CREATE INDEX noise_segments_geom_idx ON noise_segments USING gist (geom);

CREATE TABLE pois (
    id     bigserial PRIMARY KEY,
    osm_id bigint,
    kind   text NOT NULL,  -- бары, аптеки, метро, пляжи, магазины
    name   text,
    geom   geometry(Point, 4326) NOT NULL,
    tags   jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX pois_geom_idx ON pois USING gist (geom);
CREATE INDEX pois_kind_idx ON pois (kind);

CREATE TABLE sea (
    id   bigserial PRIMARY KEY,
    geom geometry(Geometry, 4326) NOT NULL  -- полигон моря или береговая линия
);
CREATE INDEX sea_geom_idx ON sea USING gist (geom);

CREATE TABLE walk_edges (
    id       bigserial PRIMARY KEY,
    source   bigint NOT NULL,
    target   bigint NOT NULL,
    geom     geometry(LineString, 4326) NOT NULL,
    length_m real NOT NULL
);
CREATE INDEX walk_edges_geom_idx ON walk_edges USING gist (geom);
CREATE INDEX walk_edges_source_idx ON walk_edges (source);

-- Предметная модель
CREATE TABLE hotels (
    id          bigserial PRIMARY KEY,
    osm_id      bigint UNIQUE,
    name        text NOT NULL,
    geom        geometry(Point, 4326) NOT NULL,
    district    text,
    building_id bigint REFERENCES buildings (id),
    card        jsonb NOT NULL DEFAULT '{}'  -- мок базовой карточки
);
CREATE INDEX hotels_geom_idx ON hotels USING gist (geom);

CREATE TABLE facades (
    id             bigserial PRIMARY KEY,
    hotel_id       bigint NOT NULL REFERENCES hotels (id) ON DELETE CASCADE,
    azimuth_deg    real NOT NULL,
    geom           geometry(LineString, 4326) NOT NULL,
    length_m       real,
    faces_street   boolean
);
CREATE INDEX facades_hotel_idx ON facades (hotel_id);
CREATE INDEX facades_geom_idx ON facades USING gist (geom);

CREATE TABLE room_categories (
    id         bigserial PRIMARY KEY,
    hotel_id   bigint NOT NULL REFERENCES hotels (id) ON DELETE CASCADE,
    name       text NOT NULL,
    facade_ids bigint[] NOT NULL DEFAULT '{}'  -- заполняет отельер
);
CREATE INDEX room_categories_hotel_idx ON room_categories (hotel_id);

CREATE TABLE facts (
    id                 bigserial PRIMARY KEY,
    entity_type        text NOT NULL,  -- hotel | facade | facade_floor | room_category | district
    entity_id          text NOT NULL,
    characteristic_key text NOT NULL,
    value              jsonb NOT NULL,
    source             text NOT NULL,  -- osm | catastro | bcn_noise | open_meteo | computed | hotelier
    method             text NOT NULL,  -- имя инструмента и версия алгоритма
    confidence         real NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    observed_at        timestamptz NOT NULL DEFAULT now(),
    valid_until        timestamptz,
    evidence           jsonb NOT NULL DEFAULT '{}',
    status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'disputed'))
);
CREATE INDEX facts_active_idx ON facts (entity_type, entity_id, characteristic_key) WHERE status = 'active';

CREATE TABLE conflicts (
    id                 bigserial PRIMARY KEY,
    entity_type        text NOT NULL,
    entity_id          text NOT NULL,
    characteristic_key text NOT NULL,
    fact_ids           bigint[] NOT NULL,
    description        text,
    resolution         text,
    status             text NOT NULL DEFAULT 'open'
);

CREATE TABLE hotelier_questions (
    id                 bigserial PRIMARY KEY,
    hotel_id           bigint NOT NULL REFERENCES hotels (id) ON DELETE CASCADE,
    characteristic_key text NOT NULL,
    question           text NOT NULL,
    reason             text,
    status             text NOT NULL DEFAULT 'open',
    answer_text        text,
    parsed             jsonb,
    asked_at           timestamptz NOT NULL DEFAULT now(),
    answered_at        timestamptz
);
CREATE INDEX hotelier_questions_hotel_idx ON hotelier_questions (hotel_id);

CREATE TABLE bookings (
    id               bigserial PRIMARY KEY,
    hotel_id         bigint NOT NULL REFERENCES hotels (id),
    room_category_id bigint REFERENCES room_categories (id),
    wishes           jsonb NOT NULL DEFAULT '{}',
    expectations     jsonb NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now()
);

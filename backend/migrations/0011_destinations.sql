-- Цели пеших маршрутов: входы и станции метро, точки вдоль границы пляжей. Рядом с отелями их держать не нужно —
-- пляж и метро бывают в километрах, поэтому отдельная таблица, а не pois (там только 300 м от отелей).
CREATE TABLE destinations (
    id        bigserial PRIMARY KEY,
    region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id),
    kind      text NOT NULL,   -- metro | beach
    osm_id    bigint,
    name      text,
    geom      geometry(Point, 4326) NOT NULL
);
CREATE INDEX destinations_kind_idx ON destinations (kind);
CREATE INDEX destinations_geom_idx ON destinations USING gist (geom);

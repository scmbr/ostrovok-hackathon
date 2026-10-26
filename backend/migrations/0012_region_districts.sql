-- Районы и контур охвата переезжают из кода в БД: разбор отелей по районам и рамки выгрузок берутся из региона.
CREATE TABLE districts (
    id        bigserial PRIMARY KEY,
    region_id bigint NOT NULL REFERENCES regions (id),
    name      text NOT NULL,
    priority  int NOT NULL,  -- при пересечении рамок побеждает район с меньшим числом: узкие береговые идут первыми
    south     double precision NOT NULL,
    west      double precision NOT NULL,
    north     double precision NOT NULL,
    east      double precision NOT NULL,
    UNIQUE (region_id, name),
    UNIQUE (region_id, priority)
);

-- Барселона: огибающие реальных границ barrio из OSM (береговые) и примерные контуры gothic, eixample (docs/data.md).
INSERT INTO districts (region_id, name, priority, south, west, north, east) VALUES
    (1, 'vila_olimpica', 1, 41.3833, 2.1880, 41.3964, 2.2063),
    (1, 'diagonal_mar',  2, 41.3947, 2.2041, 41.4118, 2.2220),
    (1, 'barceloneta',   3, 41.3577, 2.1824, 41.3879, 2.1996),
    (1, 'poblenou',      4, 41.3915, 2.1939, 41.4075, 2.2108),
    (1, 'gothic',        5, 41.379,  2.170,  41.388,  2.182),
    (1, 'eixample',      6, 41.387,  2.160,  41.400,  2.178);

-- Контур охвата региона — прямоугольник, охватывающий все районы.
ALTER TABLE regions ADD COLUMN contour geometry(Polygon, 4326);
UPDATE regions r SET contour = (
    SELECT ST_Envelope(ST_Collect(ST_MakeEnvelope(d.west, d.south, d.east, d.north, 4326)))
    FROM districts d WHERE d.region_id = r.id);

-- Части зданий принадлежат региону, как и остальные исходные таблицы.
ALTER TABLE building_parts ADD COLUMN region_id bigint NOT NULL DEFAULT 1 REFERENCES regions (id);

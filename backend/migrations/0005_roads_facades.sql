-- Дороги OSM вокруг отелей: определяют, выходит ли фасад на улицу (этап 2), позже пригодятся для шума.
CREATE TABLE roads (
    id      bigserial PRIMARY KEY,
    osm_id  bigint NOT NULL UNIQUE,
    highway text NOT NULL,
    name    text,
    geom    geometry(LineString, 4326) NOT NULL
);
CREATE INDEX roads_geom_idx ON roads USING gist (geom);

-- Фасад: внешний контур или двор (окна во двор — отдельный фасад).
ALTER TABLE facades ADD COLUMN courtyard boolean NOT NULL DEFAULT false;

-- Сколько рёбер контура рассматривалось и сколько отброшено как глухие стены (для collect stats).
ALTER TABLE hotels ADD COLUMN contour_segments int;
ALTER TABLE hotels ADD COLUMN blind_segments int;

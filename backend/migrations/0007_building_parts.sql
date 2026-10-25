-- Части зданий кадастра с разной этажностью (башня на подиуме). Хранятся только у зданий, где
-- этажность частей различается; у остальных высота одна на весь контур (buildings.height_m).
CREATE TABLE building_parts (
    id          bigserial PRIMARY KEY,
    building_id bigint NOT NULL REFERENCES buildings (id) ON DELETE CASCADE,
    geom        geometry(MultiPolygon, 4326) NOT NULL,
    floors      int NOT NULL,
    height_m    real NOT NULL
);
CREATE INDEX building_parts_building_idx ON building_parts (building_id);
CREATE INDEX building_parts_geom_idx ON building_parts USING gist (geom);
CREATE INDEX building_parts_geom_utm_idx ON building_parts USING gist (ST_Transform(geom, 25831));

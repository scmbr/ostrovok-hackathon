-- Фасадный шум карты Open Data BCN (2017): сегменты фасадов зданий отелей с уровнями в дБ.
-- Грузятся только здания отелей (catastro_ref), связь с фасадами отеля — по геометрии (collect noise-night).
CREATE TABLE noise_facades (
    id           bigserial PRIMARY KEY,
    region_id    bigint NOT NULL DEFAULT 1 REFERENCES regions (id),
    catastro_ref text NOT NULL,
    geom         geometry(LineString, 4326) NOT NULL,
    ln_db        real NOT NULL,  -- ночь (Ln), все источники
    lden_db      real,
    oci_ln_db    real,           -- ночная жизнь (Oci_Ln)
    map_year     int NOT NULL DEFAULT 2017
);
CREATE INDEX noise_facades_ref_idx ON noise_facades (catastro_ref);
CREATE INDEX noise_facades_geom_idx ON noise_facades USING gist (geom);
CREATE INDEX noise_facades_geom_utm_idx ON noise_facades USING gist (ST_Transform(geom, 25831));

-- Кадастр Испании как основной источник высот: здания кадастра лежат в buildings отдельными
-- строками (osm_id NULL, catastro_ref заполнен), OSM-здания остаются вторым источником.
CREATE UNIQUE INDEX buildings_catastro_ref_key ON buildings (catastro_ref) WHERE catastro_ref IS NOT NULL;

-- hotels.building_id — основное здание (кадастр, если найдено, иначе OSM);
-- osm_building_id — здание OSM, чтобы сравнивать этажность двух источников.
ALTER TABLE hotels ADD COLUMN osm_building_id bigint REFERENCES buildings(id);

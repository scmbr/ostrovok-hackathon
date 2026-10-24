-- Повторный сбор обновляет здания по osm_id, а не дублирует их.
CREATE UNIQUE INDEX buildings_osm_id_key ON buildings (osm_id);

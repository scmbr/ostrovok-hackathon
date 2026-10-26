-- Части зданий для демо-сида: только те, что нужны, чтобы пересчитать sea_view у демо-отелей из сида.
--   1. части, пересекающие лучи веера (±45°, шаг 5°) от середины каждого фасада демо-отеля
--      до береговой линии + 150 м (параметры sea_view по умолчанию);
--   2. все части зданий самих демо-отелей: по ним считается этажность фасада.
-- Результат — таблица seed_building_parts; цель make seed дампит её под именем building_parts и удаляет.
-- Замер на 26.10.2026: 9 357 частей из 87 673, ≈ 1,2 МБ в gzip; пересчёт sea_view по ним совпал с полным набором у 427 из 427 фасадов.
DROP TABLE IF EXISTS seed_building_parts;
CREATE TABLE seed_building_parts (LIKE building_parts INCLUDING DEFAULTS, FOREIGN KEY (building_id) REFERENCES buildings (id));

CREATE TEMP TABLE seed_part_ids AS
WITH f AS (
  SELECT f.id, ST_Transform(ST_LineInterpolatePoint(f.geom, 0.5), 25831) AS o, f.azimuth_deg AS az
  FROM facades f JOIN hotels h ON h.id = f.hotel_id WHERE h.demo),
a AS (SELECT f.id AS fid, f.o, radians(f.az + d) AS ang FROM f, generate_series(-45, 45, 5) AS d),
r AS (SELECT fid, o, ang, ST_MakeLine(o, ST_Translate(o, sin(ang) * 1500, cos(ang) * 1500)) AS line FROM a),
c AS (
  SELECT r.fid, r.o, r.ang, min(ST_Distance(r.o, p.pt)) AS d
  FROM r JOIN sea s ON s.geom && ST_Transform(ST_Expand(r.line, 1), 4326)
  CROSS JOIN LATERAL (SELECT (ST_Dump(ST_CollectionExtract(ST_Intersection(r.line, ST_Transform(s.geom, 25831)), 1))).geom AS pt) p
  GROUP BY r.fid, r.o, r.ang),
seg AS (SELECT ST_MakeLine(o, ST_Translate(o, sin(ang) * LEAST(d + 150, 1500), cos(ang) * LEAST(d + 150, 1500))) AS line FROM c)
SELECT DISTINCT p.id FROM seg JOIN building_parts p ON ST_Intersects(ST_Transform(p.geom, 25831), seg.line)
UNION
SELECT p.id FROM building_parts p JOIN hotels h ON h.building_id = p.building_id AND h.demo;

INSERT INTO seed_building_parts SELECT p.* FROM building_parts p WHERE p.id IN (SELECT id FROM seed_part_ids) ORDER BY p.id;
SELECT count(*) AS seed_building_parts FROM seed_building_parts;

-- Здание OSM, продублированное зданием кадастра (перекрытие больше половины своей площади):
-- как препятствие для лучей учитывается кадастровое, иначе высота считалась бы дважды.
ALTER TABLE buildings ADD COLUMN shadowed boolean NOT NULL DEFAULT false;

-- Лучи для sea_view ищут здания в метровой системе координат.
CREATE INDEX buildings_geom_utm_idx ON buildings USING gist (ST_Transform(geom, 25831));

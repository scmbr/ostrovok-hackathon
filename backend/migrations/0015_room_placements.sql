-- Размещение номеров: категория → фасад, диапазон этажей, число номеров. Сообщает отельер (имитация):
-- строка ссылается на свидетельство room_placement. Заменяет room_categories.facade_ids.
ALTER TABLE room_categories ADD COLUMN rooms_count int NOT NULL DEFAULT 0;
ALTER TABLE room_categories DROP COLUMN facade_ids;
CREATE UNIQUE INDEX room_categories_hotel_name_idx ON room_categories (hotel_id, name);

CREATE TABLE room_placements (
    id               bigserial PRIMARY KEY,
    room_category_id bigint NOT NULL REFERENCES room_categories (id) ON DELETE CASCADE,
    facade_id        bigint NOT NULL REFERENCES facades (id) ON DELETE CASCADE,
    floor_from       int NOT NULL CHECK (floor_from >= 1),
    floor_to         int NOT NULL,
    rooms_count      int NOT NULL CHECK (rooms_count > 0),
    source           text NOT NULL DEFAULT 'hotelier',
    confidence       real NOT NULL CHECK (confidence > 0 AND confidence < 1),
    evidence_id      bigint REFERENCES evidence (id),
    CHECK (floor_to >= floor_from)
);
CREATE INDEX room_placements_category_idx ON room_placements (room_category_id);

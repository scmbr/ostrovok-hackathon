-- Демо-подмножество отелей (40–60): остальные остаются в БД, но не участвуют в демо.
ALTER TABLE hotels ADD COLUMN demo boolean NOT NULL DEFAULT false;
CREATE INDEX hotels_demo_idx ON hotels (id) WHERE demo;

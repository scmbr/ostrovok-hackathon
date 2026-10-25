-- Конфликт возникает между свидетельствами одного факта (факт — итог их сложения), поэтому ссылаемся и на них.
ALTER TABLE conflicts ADD COLUMN evidence_ids bigint[] NOT NULL DEFAULT '{}';
ALTER TABLE conflicts ADD COLUMN detected_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX conflicts_open_idx ON conflicts (entity_type, entity_id, characteristic_key) WHERE status = 'open';

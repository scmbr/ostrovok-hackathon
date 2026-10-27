-- Причина закрытия конфликта: conflicts.resolution (текст) уже есть, добавляется машинный вид причины.
-- agreed | weight_dropped | below_threshold | evidence_removed (domain.Resolution*).
ALTER TABLE conflicts ADD COLUMN resolution_kind text;

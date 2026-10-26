-- Вопрос отельеру может касаться не отеля, а его категории номеров (название «Sea View» спорит с расчётом).
-- entity_type / entity_id пусты у вопросов об отеле; живой вопрос уникален по отелю, характеристике и сущности.
ALTER TABLE hotelier_questions ADD COLUMN entity_type text, ADD COLUMN entity_id text;

DROP INDEX hotelier_questions_live_uq;
CREATE UNIQUE INDEX hotelier_questions_live_uq ON hotelier_questions (hotel_id, characteristic_key, COALESCE(entity_id, ''))
    WHERE status IN ('pending', 'pending_low_value', 'open', 'needs_clarification');

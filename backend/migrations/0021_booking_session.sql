-- Этап 6.3: бронь из диалога. session_id — сессия, из которой гость пришёл; wishes — пожелание к номеру
-- (фасад, этаж) и статус его подтверждения отелем; expectations — снимок контракта ожиданий на момент брони.
-- wish_question_id — вопрос отельеру wish_guarantee, который сопровождает бронь с пожеланием.
ALTER TABLE bookings
    ADD COLUMN session_id       text,
    ADD COLUMN wish_question_id bigint REFERENCES hotelier_questions (id) ON DELETE SET NULL;
CREATE INDEX bookings_session_idx ON bookings (session_id);

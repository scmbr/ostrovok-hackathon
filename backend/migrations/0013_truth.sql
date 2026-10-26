-- Движок правды: признаки расхождения и устаревания у итогового факта.
ALTER TABLE facts ADD COLUMN weak_disagreement boolean NOT NULL DEFAULT false;  -- «против» есть, но слабее порога конфликта
ALTER TABLE facts ADD COLUMN stale boolean NOT NULL DEFAULT false;              -- свидетельство факта старше valid_until

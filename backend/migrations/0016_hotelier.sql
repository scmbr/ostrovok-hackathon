-- Этап 5.2: общение с отельером. Персоны имитированных отельеров, спрос на характеристики (мок),
-- скрытая «истина» мока по непроверяемым характеристикам и расширение hotelier_questions.

-- Персона имитированного отельера демо-отеля: honest | lazy | embellishing.
ALTER TABLE hotels ADD COLUMN hotelier_persona text CHECK (hotelier_persona IN ('honest', 'lazy', 'embellishing'));

-- Спрос: сколько гостей искали характеристику или включили её в контракт ожиданий. Пока мок (source = 'mock'),
-- на этапе 6 пополняется поиском.
CREATE TABLE characteristic_demand (
    characteristic_key text   NOT NULL,
    region_id          bigint NOT NULL REFERENCES regions (id),
    count              int    NOT NULL CHECK (count >= 0),
    source             text   NOT NULL,
    PRIMARY KEY (characteristic_key, region_id)
);

-- Скрытая истина мока: настоящие значения непроверяемых характеристик отеля. Не свидетельства: по ней отвечают
-- имитированные отельеры, и по ней на этапе 8 считается точность. В рантайме приложения не читается.
CREATE TABLE mock_hotel_truth (
    hotel_id           bigint NOT NULL REFERENCES hotels (id) ON DELETE CASCADE,
    characteristic_key text   NOT NULL,
    value              jsonb  NOT NULL,
    PRIMARY KEY (hotel_id, characteristic_key)
);

-- Вопрос отельеру.
-- status: pending (ждёт бюджета), pending_low_value (ниже порога ценности), open (отправлен), needs_clarification
-- (ответ получен, но разобран неуверенно), answered, closed (данные появились сами или вопрос устарел).
-- format: confirm (подтвердить расчёт: да / нет / уточнить) | open (конкретный вопрос со свободным ответом).
ALTER TABLE hotelier_questions
    ADD COLUMN format   text CHECK (format IN ('confirm', 'open')),
    ADD COLUMN sent_at  timestamptz,
    ADD COLUMN score    jsonb NOT NULL DEFAULT '{}'; -- спрос, неуверенность, влияние, число гостей
-- Уже созданные агентом-сборщиком открытые вопросы считаются отправленными в момент создания.
UPDATE hotelier_questions SET sent_at = asked_at WHERE status = 'open';

-- Один живой вопрос на характеристику отеля (а не только открытый): копящиеся не плодят дубли.
DROP INDEX hotelier_questions_open_uq;
CREATE UNIQUE INDEX hotelier_questions_live_uq ON hotelier_questions (hotel_id, characteristic_key)
    WHERE status IN ('pending', 'pending_low_value', 'open', 'needs_clarification');

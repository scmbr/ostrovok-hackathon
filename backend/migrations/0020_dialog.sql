-- Этап 6.2: журнал неудовлетворённых желаний и сессии диалога.
-- Желание гостя, которого нет в реестре; повторы увеличивают count. proposal — предложение новой характеристики,
-- только записывается (в реестр само не попадает).
CREATE TABLE unmet_wishes (
    id         bigserial   PRIMARY KEY,
    region_id  bigint      NOT NULL REFERENCES regions (id),
    text       text        NOT NULL,
    normalized text        NOT NULL,
    count      int         NOT NULL DEFAULT 1,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    proposal   jsonb,
    UNIQUE (region_id, normalized)
);

-- Состояние диалога лежит в БД, а не в памяти процесса: экземпляров сервера может быть несколько, и перезапуск
-- не должен терять разговор. Живёт час после последнего обращения, устаревшие строки удаляются при чтении.
CREATE TABLE dialog_sessions (
    session_id text        PRIMARY KEY,
    state      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dialog_sessions_updated_idx ON dialog_sessions (updated_at);

-- Журнал диалога: режим «rule» уже есть (0018); у agent_trace hotel_id необязателен.

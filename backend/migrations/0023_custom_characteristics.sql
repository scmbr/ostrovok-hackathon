-- Характеристики, добавленные текстом из интерфейса «под капотом»: реестр на старте = встроенный YAML + эти строки.
-- Конфликт ключа со встроенной характеристикой — ошибка старта, а не тихая перезапись.
CREATE TABLE custom_characteristics (
    key         text PRIMARY KEY,
    region_id   bigint NOT NULL REFERENCES regions (id),
    yaml        text NOT NULL,
    origin_text text NOT NULL,
    added_at    date NOT NULL DEFAULT CURRENT_DATE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

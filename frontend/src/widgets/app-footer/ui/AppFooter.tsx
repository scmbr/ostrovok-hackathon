// Декоративный футер: ссылки ведут никуда, так задумано (прототип).
const COLUMNS: { title: string; items: string[] }[] = [
  { title: 'Компания', items: ['О нас', 'Карьера', 'Пресс-центр', 'Контакты'] },
  { title: 'Гостям', items: ['Как это работает', 'Программа «Гуру»', 'Помощь', 'Отмена брони'] },
  { title: 'Партнёрам', items: ['Для отельеров', 'Подключить отель', 'API', 'Правила размещения'] },
]

export function AppFooter() {
  return (
    <footer className="border-t border-line bg-surface">
      <div className="mx-auto grid max-w-7xl gap-8 px-4 py-10 md:grid-cols-[2fr_1fr_1fr_1fr]">
        <div>
          <p className="font-display text-title text-ink">Бронь без сюрпризов</p>
          <p className="mt-2 max-w-xs text-caption text-ink-muted">
            Прототип хакатона Островка. Узнаём об отелях то, чего нет в карточках, и говорим, откуда это известно.
          </p>
        </div>
        {COLUMNS.map((c) => (
          <nav key={c.title} aria-label={c.title}>
            <p className="text-body font-medium text-ink">{c.title}</p>
            <ul className="mt-3 space-y-2">
              {c.items.map((i) => (
                <li key={i}>
                  <span className="text-caption text-ink-muted">{i}</span>
                </li>
              ))}
            </ul>
          </nav>
        ))}
      </div>
      <div className="border-t border-line">
        <p className="mx-auto max-w-7xl px-4 py-4 text-caption text-ink-muted">
          © 2026 Бронь без сюрпризов · демонстрационный прототип, оплаты и реальных броней нет
        </p>
      </div>
    </footer>
  )
}

import { useEffect, useState } from 'react'
import type { Characteristic, Compare, CompareDiff, CompareSide } from '@/shared/api'
import { Badge, Button, Card } from '@/shared/ui'
import { getCharacteristics } from '@/entities/fact'
import { AskHotelButton } from '@/features/ask-hotelier'

interface Props {
  data: Compare
  onBook?: (side: CompareSide) => void
}

// Таблица различий: колонка на вариант; лучший по характеристике отмечен. Тексты приходят с сервера готовыми.
function DiffTable({ data, diffs }: { data: Compare; diffs: CompareDiff[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-line text-xs text-ink-muted">
            <th className="py-1 pr-3 font-medium">Характеристика</th>
            {data.sides.map((s, i) => (
              <th key={i} className="py-1 pr-3 font-medium">
                {s.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {diffs.map((d) => (
            <tr key={d.key} className="border-b border-line align-top">
              <td className="py-1.5 pr-3 font-medium">{d.title}</td>
              {data.sides.map((_, i) => (
                <td key={i} className={`py-1.5 pr-3 ${d.better === i ? 'font-medium text-ok' : 'text-ink'}`}>
                  {d.better === i && '✓ '}
                  {d.texts[i] ?? '—'}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function CompareView({ data, onBook }: Props) {
  const [registry, setRegistry] = useState<Characteristic[]>([])
  useEffect(() => {
    getCharacteristics().then(setRegistry, () => setRegistry([]))
  }, [])
  const titleOf = (key: string) => registry.find((c) => c.key === key)?.title ?? key
  const byKey = new Map([...data.differences, ...data.same].map((d) => [d.key, d]))
  return (
    <div className="space-y-4">
      <Card className="border-brand bg-brand-tint">
        <h2 className="text-base font-semibold text-brand-link">Вывод</h2>
        <p className="mt-1 text-sm text-brand-link">{data.verdict}</p>
        {data.wishes.length > 0 && <p className="mt-1 text-xs text-ink">Сравнение относительно ваших желаний: {data.wishes.map(titleOf).join(', ')}</p>}
      </Card>

      <div className={`grid gap-3 ${data.sides.length > 2 ? 'md:grid-cols-3' : 'md:grid-cols-2'}`}>
        {data.sides.map((s, i) => (
          <Card key={i} className={data.winner === i ? 'ring-2 ring-ok' : ''}>
            <div className="flex items-start gap-2">
              <div>
                <h3 className="font-semibold">{s.label}</h3>
                <p className="text-sm text-ink">
                  {s.category} · <span title="Цена — мок: базовая карточка Островка заменена генератором">{s.price_eur} € за ночь</span>
                </p>
              </div>
              {data.winner === i && <Badge tone="green">лучше для вас</Badge>}
            </div>
            {s.guaranteed && <p className="mt-1 text-xs text-ok">Все желания гарантированы</p>}
            {s.conflicts.map((c, k) => (
              <p key={k} className="mt-1 text-xs text-warn">
                Под вопросом (источники спорят): {c}
              </p>
            ))}
            {onBook && (
              <Button small variant="primary" className="mt-2" onClick={() => onBook(s)}>
                Контракт и бронь
              </Button>
            )}
          </Card>
        ))}
      </div>

      {data.violations.some((v) => v.length > 0) && (
        <Card>
          <h3 className="font-semibold">Нарушены жёсткие пожелания</h3>
          <ul className="mt-1 space-y-0.5 text-sm text-bad">
            {data.violations.map((v, i) =>
              v.length ? (
                <li key={i}>
                  {data.sides[i]?.label}: {v.join(', ')}
                </li>
              ) : null,
            )}
          </ul>
        </Card>
      )}

      {data.differences.length > 0 && (
        <Card>
          <h3 className="mb-2 font-semibold">Существенные различия</h3>
          <DiffTable data={data} diffs={data.differences} />
        </Card>
      )}

      {data.tradeoff && (
        <Card>
          <h3 className="font-semibold">Цена компромисса</h3>
          <p className="mt-1 text-sm">{data.tradeoff}</p>
        </Card>
      )}

      {data.same.length > 0 && (
        <p className="text-sm text-ink">
          Одинаково (ниже порога значимости): {data.same.map((d) => d.title).join(', ')}.
        </p>
      )}

      {data.uncertain.length > 0 && (
        <Card>
          <h3 className="font-semibold">Неуверенное</h3>
          <ul className="mt-1 divide-y divide-line">
            {data.uncertain.map((u, i) => (
              <li key={i} className="flex flex-wrap items-center gap-2 py-1.5 text-sm">
                <span className="font-medium">{data.sides[u.side]?.label}</span>
                <span>{u.title}:</span>
                <span className="text-ink">{u.reason}</span>
                <span className="ml-auto">{u.action === 'ask_hotel' && <AskHotelButton hotelId={u.hotel_id} charKey={u.key} />}</span>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {data.days.length > 0 && (
        <Card>
          <h3 className="mb-2 font-semibold">Как пройдёт ваш день</h3>
          <div className="space-y-3">
            {data.days.map((g) => {
              const rows = g.keys.map((k) => byKey.get(k)).filter((d): d is CompareDiff => !!d)
              if (!rows.length) return null
              return (
                <div key={g.part}>
                  <h4 className="text-xs font-semibold uppercase tracking-wide text-ink-muted">{g.title}</h4>
                  <DiffTable data={data} diffs={rows} />
                </div>
              )
            })}
          </div>
        </Card>
      )}
    </div>
  )
}

import type { ReactNode } from 'react'
import type { Explanation, SearchHotel, Variant } from '@/shared/api'
import { Badge, Card } from '@/shared/ui'
import { pct } from '@/shared/lib'
import { WishRow } from './WishRow'

interface Props {
  hotel: SearchHotel
  explanation?: Explanation
  highlighted?: boolean
  onHover?: (id: number | null) => void
  // Кнопки отеля (в шортлист, похожий, подробнее) подставляет виджет: сущность не знает о функциях.
  actions?: ReactNode
  // Кнопки варианта номера (контракт, бронь).
  variantActions?: (v: Variant) => ReactNode
}

function VariantBlock({ v, title, actions }: { v: Variant; title: string; actions?: ReactNode }) {
  return (
    <div className="mt-3 rounded-md bg-slate-50 p-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-xs uppercase tracking-wide text-slate-500">{title}</span>
        <b>{v.name}</b>
        <span title="Цена — мок: базовая карточка Островка заменена генератором">{v.price_eur} € за ночь</span>
        {v.guaranteed && <Badge tone="green">всё гарантировано</Badge>}
        <div className="ml-auto flex gap-2">{actions}</div>
      </div>
      <ul className="mt-1 divide-y divide-slate-200">
        {v.wishes.map((w) => (
          <WishRow key={w.key} wish={w} />
        ))}
      </ul>
    </div>
  )
}

export function HotelResultCard({ hotel, explanation, highlighted, onHover, actions, variantActions }: Props) {
  return (
    <div onMouseEnter={() => onHover?.(hotel.hotel_id)} onMouseLeave={() => onHover?.(null)}>
      <Card className={highlighted ? 'ring-2 ring-sky-500' : ''}>
        <div className="flex flex-wrap items-start gap-2">
          <div>
            <h3 className="text-base font-semibold">{hotel.name}</h3>
            <div className="text-xs text-slate-500">
              {hotel.district} · {'★'.repeat(hotel.card.stars)} · оценка {pct(hotel.score)}
              {hotel.card.mock && ' · карточка — мок'}
            </div>
          </div>
          <div className="ml-auto flex flex-wrap gap-2">{actions}</div>
        </div>

        <VariantBlock v={hotel.best} title="лучший вариант" actions={variantActions?.(hotel.best)} />
        {hotel.alternative && (
          <VariantBlock v={hotel.alternative} title="альтернатива" actions={variantActions?.(hotel.alternative)} />
        )}

        {hotel.other_conflicts.map((c) => (
          <div key={`${c.category}-${c.key}`} className="mt-2 rounded bg-amber-50 p-2 text-xs text-amber-900 ring-1 ring-inset ring-amber-200">
            Категория «{c.category}»: под вопросом (источники спорят). {c.description}
          </div>
        ))}

        {explanation && (
          <details className="mt-2 text-sm">
            <summary className="cursor-pointer text-xs text-sky-800">Как выбрано</summary>
            <p className="mt-1 font-medium">{explanation.headline}</p>
            <ul className="ml-4 list-disc text-slate-700">
              {explanation.lines.map((l, i) => (
                <li key={i}>{l}</li>
              ))}
            </ul>
            {explanation.notes.map((n, i) => (
              <p key={i} className="mt-1 text-xs text-amber-900">
                {n}
              </p>
            ))}
          </details>
        )}
      </Card>
    </div>
  )
}

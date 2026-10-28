import type { ReactNode } from 'react'
import type { Explanation, SearchHotel, Variant } from '@/shared/api'
import { Card, Icon } from '@/shared/ui'
import { nightsWord, pct } from '@/shared/lib'
import { WishRow } from './WishRow'
import { HotelThumb, RoomThumb } from './HotelPhoto'

interface Props {
  hotel: SearchHotel
  explanation?: Explanation
  highlighted?: boolean
  onHover?: (id: number | null) => void
  // Кнопки отеля (в шортлист, похожий, подробнее) подставляет виджет: сущность не знает о функциях.
  actions?: ReactNode
  // Кнопки варианта номера (контракт, бронь).
  variantActions?: (v: Variant) => ReactNode
  // Число ночей выбранных дат: цена показывается за весь срок.
  nights?: number
}

function VariantBlock({ v, title, actions, nights }: { v: Variant; title: string; actions?: ReactNode; nights?: number }) {
  return (
    <div className="mt-3 rounded-inner bg-surface-muted p-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <RoomThumb category={v.name} />
        <span className="text-xs uppercase tracking-wide text-ink-muted">{title}</span>
        <b>{v.name}</b>
        <span className="text-price" title="Цена — мок: базовая карточка Островка заменена генератором">
          {nights && nights > 0 ? (
            <>
              {v.price_eur * nights} €
              <span className="ml-1 text-caption font-normal text-ink-muted">
                за {nights} {nightsWord(nights)} · {v.price_eur} € за ночь
              </span>
            </>
          ) : (
            <>
              {v.price_eur} €<span className="ml-1 text-caption font-normal text-ink-muted">за ночь</span>
            </>
          )}
        </span>
        {v.guaranteed && (
          <span className="inline-flex items-center gap-1 rounded-sm bg-grad-promo px-2 py-0.5 text-caption font-medium text-ink">
            <Icon name="check" size={12} />
            всё гарантировано
          </span>
        )}
        <div className="ml-auto flex gap-2">{actions}</div>
      </div>
      <ul className="mt-1 divide-y divide-line">
        {v.wishes.map((w) => (
          <WishRow key={w.key} wish={w} />
        ))}
      </ul>
    </div>
  )
}

export function HotelResultCard({ hotel, explanation, highlighted, onHover, actions, variantActions, nights }: Props) {
  return (
    <div onMouseEnter={() => onHover?.(hotel.hotel_id)} onMouseLeave={() => onHover?.(null)}>
      <Card className={`!p-1 ${highlighted ? 'ring-2 ring-brand' : ''}`}>
        <div className="flex items-start gap-2">
          <HotelThumb hotelId={hotel.hotel_id} name={hotel.name} />
          <div className="flex min-w-0 flex-1 flex-wrap items-start gap-2">
          <div>
            <h3 className="text-title">{hotel.name}</h3>
            <div className="text-caption text-ink-muted">
              {hotel.district} · {'★'.repeat(hotel.card.stars)} · оценка {pct(hotel.score)}
              {hotel.card.mock && ' · карточка — мок'}
            </div>
          </div>
          <div className="ml-auto flex flex-wrap gap-2">{actions}</div>
          </div>
        </div>

        <VariantBlock v={hotel.best} title="лучший вариант" actions={variantActions?.(hotel.best)} nights={nights} />
        {hotel.alternative && (
          <VariantBlock v={hotel.alternative} title="альтернатива" actions={variantActions?.(hotel.alternative)} nights={nights} />
        )}

        {hotel.other_conflicts.map((c) => (
          <div key={`${c.category}-${c.key}`} className="mt-2 rounded bg-warn-bg p-2 text-xs text-warn ring-1 ring-inset ring-warn/30">
            Категория «{c.category}»: под вопросом (источники спорят). {c.description}
          </div>
        ))}

        {explanation && (
          <details className="mt-2 text-sm">
            <summary className="cursor-pointer text-xs text-brand-link">Как выбрано</summary>
            <p className="mt-1 font-medium">{explanation.headline}</p>
            <ul className="ml-4 list-disc text-ink">
              {explanation.lines.map((l, i) => (
                <li key={i}>{l}</li>
              ))}
            </ul>
            {explanation.notes.map((n, i) => (
              <p key={i} className="mt-1 text-xs text-warn">
                {n}
              </p>
            ))}
          </details>
        )}
      </Card>
    </div>
  )
}

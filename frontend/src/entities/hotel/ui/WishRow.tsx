import { useState } from 'react'
import type { WishEval } from '@/shared/api'
import { authorLabel, formatDate, pct, sourceLabel, valueLabel } from '@/shared/lib'
import { WishBadge } from './WishBadge'

// Одно желание гостя в карточке выдачи: значок статуса, а «откуда известно» — по раскрытию.
export function WishRow({ wish }: { wish: WishEval }) {
  const [open, setOpen] = useState(false)
  const hasDetails = wish.sources.length > 0 || wish.conflict || wish.caveat || wish.observed_at
  return (
    <li className="py-1.5">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="font-medium">{wish.title}</span>
        {wish.importance === 'must' && <span className="text-xs text-slate-500">обязательно</span>}
        <WishBadge wish={wish} />
        {wish.value !== undefined && wish.value !== null && wish.status !== 'unknown' && (
          <span className="text-slate-600">{typeof wish.value === 'object' ? '' : valueLabel(wish.key, wish.value)}</span>
        )}
        {hasDetails && (
          <button type="button" className="ml-auto text-xs text-sky-800 hover:underline" onClick={() => setOpen(!open)}>
            {open ? 'скрыть' : 'откуда известно'}
          </button>
        )}
      </div>
      {wish.booking_wish && (
        <div className="mt-1 text-xs text-slate-700">
          Пожелание к брони: <b>{wish.booking_wish.text}</b>, ~{pct(wish.booking_wish.probability)}
          {wish.booking_wish.hotel_confirmed ? ' (отель подтвердил)' : ' (отель ещё не подтвердил)'}
        </div>
      )}
      {wish.conflict && <div className="mt-1 text-xs text-amber-800">Источники спорят: {wish.conflict}</div>}
      {wish.caveat && <div className="mt-1 text-xs text-slate-600">{wish.caveat}</div>}
      {open && (
        <div className="mt-1 rounded bg-slate-50 p-2 text-xs text-slate-700">
          <div>
            Уверенность {pct(wish.confidence)}
            {wish.observed_at && <> · получено {formatDate(wish.observed_at)}</>}
            {wish.valid_until && <> · актуально до {formatDate(wish.valid_until)}</>}
          </div>
          <ul className="mt-1 space-y-0.5">
            {wish.sources.map((s) => (
              <li key={s.id}>
                {sourceLabel(s.source)} ({authorLabel(s.author_type)}), надёжность {pct(s.reliability)}, вклад{' '}
                {s.contribution >= 0 ? '+' : ''}
                {s.contribution.toFixed(2)}, {formatDate(s.observed_at)}
              </li>
            ))}
          </ul>
        </div>
      )}
    </li>
  )
}

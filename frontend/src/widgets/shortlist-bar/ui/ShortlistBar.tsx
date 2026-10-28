import { Link } from 'react-router-dom'
import { removeHotel, useShortlist } from '@/features/shortlist'
import { compareHref } from '@/features/compare-hotels'

// Панель шортлиста: вход в сравнение (2–3 отеля).
export function ShortlistBar() {
  const { hotels, error } = useShortlist()
  if (!hotels.length && !error) return null
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-card border border-line bg-white px-3 py-2 text-sm ">
      <b>Шортлист</b>
      {hotels.map((h) => (
        <span key={h.id} className="inline-flex items-center gap-1 rounded-full bg-surface-muted px-2 py-0.5">
          <Link to={`/hotel/${h.id}`} className="hover:underline">
            {h.name}
          </Link>
          <button type="button" aria-label={`Убрать ${h.name}`} className="text-ink-muted hover:text-bad" onClick={() => void removeHotel(h.id)}>
            ✕
          </button>
        </span>
      ))}
      {hotels.length >= 2 && (
        <Link
          to={compareHref(hotels.slice(0, 3).map((h) => h.id))}
          className="ml-auto rounded-inner bg-brand px-3 py-1 text-sm font-medium text-white hover:opacity-90"
        >
          Сравнить{hotels.length > 3 ? ' первые 3' : ''}
        </Link>
      )}
      {hotels.length === 1 && <span className="ml-auto text-xs text-ink-muted">Добавьте ещё отель, чтобы сравнить</span>}
      {error && <span className="text-xs text-bad">{error}</span>}
    </div>
  )
}

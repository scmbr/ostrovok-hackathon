import { Link } from 'react-router-dom'
import { removeHotel, useShortlist } from '@/features/shortlist'
import { compareHref } from '@/features/compare-hotels'

// Панель шортлиста: вход в сравнение (2–3 отеля).
export function ShortlistBar() {
  const { hotels, error } = useShortlist()
  if (!hotels.length && !error) return null
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm shadow-sm">
      <b>Шортлист</b>
      {hotels.map((h) => (
        <span key={h.id} className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2 py-0.5">
          <Link to={`/hotel/${h.id}`} className="hover:underline">
            {h.name}
          </Link>
          <button type="button" aria-label={`Убрать ${h.name}`} className="text-slate-500 hover:text-rose-700" onClick={() => void removeHotel(h.id)}>
            ✕
          </button>
        </span>
      ))}
      {hotels.length >= 2 && (
        <Link
          to={compareHref(hotels.slice(0, 3).map((h) => h.id))}
          className="ml-auto rounded-md bg-sky-700 px-3 py-1 text-sm font-medium text-white hover:bg-sky-800"
        >
          Сравнить{hotels.length > 3 ? ' первые 3' : ''}
        </Link>
      )}
      {hotels.length === 1 && <span className="ml-auto text-xs text-slate-500">Добавьте ещё отель, чтобы сравнить</span>}
      {error && <span className="text-xs text-rose-700">{error}</span>}
    </div>
  )
}

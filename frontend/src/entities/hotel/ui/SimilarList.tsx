import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { SimilarHotel } from '@/shared/api'
import { Card } from '@/shared/ui'

// Похожие отели: по профилю собранных характеристик, не по звёздам и цене. Главные отличия — готовые названия характеристик.
export function SimilarList({ hotels, actions }: { hotels: SimilarHotel[]; actions?: (h: SimilarHotel) => ReactNode }) {
  if (!hotels.length) return <p className="text-sm text-ink">Похожих отелей не нашлось.</p>
  return (
    <ul className="space-y-2">
      {hotels.map((h) => (
        <li key={h.id}>
          <Card>
            <div className="flex flex-wrap items-center gap-2">
              <Link to={`/hotel/${h.id}`} className="font-semibold text-brand-link hover:underline">
                {h.name}
              </Link>
              <span className="text-xs text-ink-muted">
                {h.district} · {'★'.repeat(h.card.stars)}
              </span>
              <span className="ml-auto flex gap-2">{actions?.(h)}</span>
            </div>
            {h.differences.length > 0 && (
              <p className="mt-1 text-xs text-ink">
                Отличается: {h.differences.map((d) => d.title + (d.unknown ? ' (данных нет)' : '')).join(', ')}
              </p>
            )}
          </Card>
        </li>
      ))}
    </ul>
  )
}

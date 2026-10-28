import type { NearbyItem } from '@/shared/api'

// Легенда по типам заведений: цвет линии, подпись и расстояние; «не нашли» — честно, без «далеко».
export function NearbyLegend({ items, radius }: { items: NearbyItem[]; radius: number }) {
  if (!items.length) return null
  return (
    <ul className="absolute bottom-3 left-3 z-[1000] space-y-1 rounded-card bg-white/95 px-3 py-2 text-xs shadow">
      {items.map((it) => (
        <li key={it.id} className="flex items-center gap-2">
          <span className="inline-block h-0 w-5 border-t-[3px] border-dashed" style={{ borderColor: it.color }} />
          <span className="font-medium">{it.title}</span>
          <span className="text-ink-muted">{it.found && it.nearby ? `~${it.nearby.distance_m} м по прямой` : `в OSM не нашли в ${radius} м`}</span>
        </li>
      ))}
    </ul>
  )
}

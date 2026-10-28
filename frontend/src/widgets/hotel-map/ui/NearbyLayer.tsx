import { Fragment } from 'react'
import { Marker, Polyline, Tooltip } from 'react-leaflet'
import type { NearbyItem } from '@/shared/api'
import { poiMarkerIcon } from '../lib/poiIcon'

// Пунктир от отеля до ближайшего заведения каждого запрошенного типа, маркер с иконкой и подписью «~N м по прямой».
export function NearbyLayer({ from, items }: { from: [number, number]; items: NearbyItem[] }) {
  return (
    <>
      {items.map((it) => {
        const n = it.nearby
        if (!it.found || !n) return null
        return (
          <Fragment key={it.id}>
            <Polyline positions={[from, [n.lat, n.lon]]} pathOptions={{ color: it.color, weight: 3, dashArray: '6 8', opacity: 0.9 }} />
            <Marker position={[n.lat, n.lon]} icon={poiMarkerIcon(it.icon, it.color)}>
              <Tooltip permanent direction="top" offset={[0, -12]}>
                {n.label}
                {n.name ? ` «${n.name}»` : ''} · ~{n.distance_m} м по прямой
              </Tooltip>
            </Marker>
          </Fragment>
        )
      })}
    </>
  )
}

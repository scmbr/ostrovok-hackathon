import { useEffect, useMemo, useState } from 'react'
import { CircleMarker, MapContainer, TileLayer, Tooltip, useMap } from 'react-leaflet'
import { latLngBounds } from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { getHotel, getNearby } from '@/entities/hotel'
import type { NearbyItem } from '@/shared/api'
import { NearbyLayer } from './NearbyLayer'
import { NearbyLegend } from './NearbyLegend'

export interface MapHotel {
  id: number
  name: string
}

interface Point extends MapHotel {
  lat: number
  lon: number
}

// Координаты отелей выдачи: выдача их не содержит, берём из карточек (кеш в памяти).
function usePoints(hotels: MapHotel[]): Point[] {
  const [points, setPoints] = useState<Point[]>([])
  const key = hotels.map((h) => h.id).join(',')
  useEffect(() => {
    let live = true
    Promise.all(hotels.map((h) => getHotel(h.id).then((c) => ({ id: h.id, name: h.name, lat: c.lat, lon: c.lon }), () => null))).then((ps) => {
      if (live) setPoints(ps.filter((p): p is Point => p !== null))
    })
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
  return points
}

function FitBounds({ points }: { points: Point[] }) {
  const map = useMap()
  useEffect(() => {
    if (!points.length) return
    map.fitBounds(latLngBounds(points.map((p) => [p.lat, p.lon] as [number, number])), { padding: [30, 30], maxZoom: 16 })
  }, [points, map])
  return null
}

// Ближайшие заведения запрошенных типов для выбранного или наведённого отеля. Типы накапливаются вместе с желаниями сессии.
function useNearby(hotelId: number | null | undefined, kinds: string[]): NearbyItem[] {
  const [items, setItems] = useState<NearbyItem[]>([])
  const key = kinds.join(',')
  useEffect(() => {
    if (hotelId == null || !kinds.length) {
      setItems([])
      return
    }
    let live = true
    getNearby(hotelId, kinds).then(
      (r) => live && setItems(r),
      () => live && setItems([]),
    )
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hotelId, key])
  return items
}

export const TILE_ATTRIBUTION = '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
export const TILE_URL = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png'

// Общая карта выдачи: отели маркерами, выделение синхронно с наведением на карточку.
export function ResultsMap({
  hotels,
  highlightId,
  onSelect,
  poiKinds = [],
}: {
  hotels: MapHotel[]
  highlightId?: number | null
  onSelect?: (id: number) => void
  /** Типы заведений из желаний сессии (nightclub, bar, pharmacy…): до ближайшего каждого рисуется пунктир. */
  poiKinds?: string[]
}) {
  const points = usePoints(hotels)
  const nearby = useNearby(highlightId, poiKinds)
  const hotPoint = points.find((p) => p.id === highlightId)
  const center = useMemo<[number, number]>(() => [41.3874, 2.1686], [])
  return (
    <div className="relative h-full min-h-[300px] w-full">
      <MapContainer center={center} zoom={13} className="h-full min-h-[300px] w-full rounded-card" scrollWheelZoom>
        <TileLayer attribution={TILE_ATTRIBUTION} url={TILE_URL} />
        <FitBounds points={points} />
        {hotPoint && nearby.length > 0 && <NearbyLayer from={[hotPoint.lat, hotPoint.lon]} items={nearby} />}
        {points.map((p) => {
          const hot = p.id === highlightId
          return (
            <CircleMarker
              key={p.id}
              center={[p.lat, p.lon]}
              radius={hot ? 12 : 8}
              pathOptions={{ color: hot ? '#0c4a6e' : '#0369a1', fillColor: hot ? '#f59e0b' : '#0ea5e9', fillOpacity: 0.9, weight: hot ? 3 : 2 }}
              eventHandlers={{ click: () => onSelect?.(p.id) }}
            >
              <Tooltip permanent={hot} direction="top">
                {p.name}
              </Tooltip>
            </CircleMarker>
          )
        })}
      </MapContainer>
      {hotPoint && <NearbyLegend items={nearby} radius={300} />}
    </div>
  )
}

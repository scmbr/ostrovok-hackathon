import { useEffect, useMemo, useState } from 'react'
import { CircleMarker, MapContainer, TileLayer, Tooltip, useMap } from 'react-leaflet'
import { latLngBounds } from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { getHotel } from '@/entities/hotel'

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

export const TILE_ATTRIBUTION = '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
export const TILE_URL = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png'

// Общая карта выдачи: отели маркерами, выделение синхронно с наведением на карточку.
export function ResultsMap({ hotels, highlightId, onSelect }: { hotels: MapHotel[]; highlightId?: number | null; onSelect?: (id: number) => void }) {
  const points = usePoints(hotels)
  const center = useMemo<[number, number]>(() => [41.3874, 2.1686], [])
  return (
    <MapContainer center={center} zoom={13} className="h-full min-h-[300px] w-full rounded-card" scrollWheelZoom>
      <TileLayer attribution={TILE_ATTRIBUTION} url={TILE_URL} />
      <FitBounds points={points} />
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
  )
}

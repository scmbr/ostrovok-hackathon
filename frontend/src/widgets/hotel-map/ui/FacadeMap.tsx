import { useState } from 'react'
import { CircleMarker, MapContainer, Marker, TileLayer, Tooltip } from 'react-leaflet'
import { divIcon } from 'leaflet'
import 'leaflet/dist/leaflet.css'
import type { Facade, HotelCardData } from '@/shared/api'
import { Button } from '@/shared/ui'
import { TILE_ATTRIBUTION, TILE_URL } from './ResultsMap'

type Mode = 'sea' | 'noise'

interface Style {
  color: string
  label: string
}

const GRAY: Style = { color: '#94a3b8', label: 'нет данных' }

function seaStyle(f: Facade): Style {
  const v = f.values.find((x) => x.key === 'sea_view')
  if (!v) return GRAY
  const floor = Number(v.value.min_floor)
  if (Number.isFinite(floor) && floor > 0) return { color: '#0284c7', label: `море видно с ${floor}-го этажа` }
  return { color: '#64748b', label: 'моря не видно' }
}

const NOISE: Record<string, Style> = {
  quiet: { color: '#16a34a', label: 'ночью тихо' },
  moderate: { color: '#d97706', label: 'ночью умеренно' },
  loud: { color: '#dc2626', label: 'ночью шумно' },
}

function noiseStyle(f: Facade): Style {
  const v = f.values.find((x) => x.key === 'noise_night')
  return (v && NOISE[String(v.value.level)]) || GRAY
}

// Стрелка наружу от фасада: азимут внешней нормали, цвет — по виду на море или по шуму.
function arrow(color: string, azimuth: number) {
  return divIcon({
    className: '',
    iconSize: [28, 28],
    iconAnchor: [14, 14],
    html: `<svg width="28" height="28" viewBox="-14 -14 28 28" style="transform:rotate(${azimuth}deg)"><path d="M0 -12 L7 4 L0 0 L-7 4 Z" fill="${color}" stroke="#0f172a" stroke-width="1"/></svg>`,
  })
}

export function FacadeMap({ hotel }: { hotel: HotelCardData }) {
  const [mode, setMode] = useState<Mode>('sea')
  const facades = hotel.rooms?.facades ?? []
  const styleOf = mode === 'sea' ? seaStyle : noiseStyle
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-sm">
        <Button small variant={mode === 'sea' ? 'primary' : 'secondary'} onClick={() => setMode('sea')}>
          Вид на море
        </Button>
        <Button small variant={mode === 'noise' ? 'primary' : 'secondary'} onClick={() => setMode('noise')}>
          Шум ночью
        </Button>
        <span className="text-xs text-ink-muted">Стрелка — куда смотрит фасад</span>
      </div>
      <MapContainer center={[hotel.lat, hotel.lon]} zoom={18} className="h-80 w-full rounded-card" scrollWheelZoom>
        <TileLayer attribution={TILE_ATTRIBUTION} url={TILE_URL} maxZoom={19} />
        <CircleMarker center={[hotel.lat, hotel.lon]} radius={5} pathOptions={{ color: '#0f172a', fillColor: '#f59e0b', fillOpacity: 1 }} />
        {facades.map((f) => {
          const s = styleOf(f)
          return (
            <Marker key={f.id} position={f.mid} icon={arrow(s.color, f.azimuth_deg)}>
              <Tooltip direction="top">
                {f.side} сторона{f.faces_street ? ', на улицу' : f.courtyard ? ', во двор' : ''}: {s.label}
              </Tooltip>
            </Marker>
          )
        })}
      </MapContainer>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-ink">
        {facades.map((f) => {
          const s = styleOf(f)
          return (
            <li key={f.id} className="flex items-center gap-1">
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: s.color }} />
              {f.side}: {s.label}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

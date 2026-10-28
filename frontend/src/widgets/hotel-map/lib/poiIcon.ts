import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { Beer, Coffee, MapPin, Martini, Music, Pill, Utensils, type LucideIcon } from 'lucide-react'
import { divIcon } from 'leaflet'

// Значки типов заведений: имя приходит из словаря бэкенда (registry/poi_kinds.yaml, поле icon).
const ICONS: Record<string, LucideIcon> = { music: Music, martini: Martini, beer: Beer, coffee: Coffee, utensils: Utensils, pill: Pill }

function poiIconSvg(name: string, size = 14): string {
  const Cmp = ICONS[name] ?? MapPin
  return renderToStaticMarkup(createElement(Cmp, { size, strokeWidth: 2.2, color: '#fff' }))
}

// Круглый маркер цвета типа со значком внутри.
export function poiMarkerIcon(name: string, color: string) {
  return divIcon({
    className: '',
    html: `<span style="display:flex;align-items:center;justify-content:center;width:26px;height:26px;border-radius:9999px;background:${color};border:2px solid #fff;box-shadow:0 1px 4px rgba(0,0,0,.4)">${poiIconSvg(name)}</span>`,
    iconSize: [26, 26],
    iconAnchor: [13, 13],
  })
}

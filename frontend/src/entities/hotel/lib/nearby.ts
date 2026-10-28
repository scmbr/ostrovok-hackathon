import type { Desire } from '@/shared/api'

const NEAR_POI = 'near_poi.'

// Типы заведений, которые нужны карте: желания «рядом с …» сессии и ночная аптека (линию до ближайшей аптеки любого режима
// рисуем и для неё). Порядок — как в сессии, без повторов.
export function nearbyTypes(wishes: Desire[] | undefined): string[] {
  const out: string[] = []
  for (const w of wishes ?? []) {
    const id = w.key.startsWith(NEAR_POI) ? w.key.slice(NEAR_POI.length) : w.key === 'night_pharmacy' ? 'pharmacy' : ''
    if (id && !out.includes(id)) out.push(id)
  }
  return out
}

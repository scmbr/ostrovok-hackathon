import { apiGet } from '@/shared/api'
import type { Characteristic, ExplainedFact } from '@/shared/api'

let cache: Promise<Characteristic[]> | null = null

// Реестр характеристик: названия и поле значения. Неизменен до перезапуска сервера.
export function getCharacteristics(): Promise<Characteristic[]> {
  if (!cache) {
    cache = apiGet<{ characteristics: Characteristic[] }>('/characteristics').then((r) => r.characteristics)
    cache.catch(() => (cache = null))
  }
  return cache
}

// Факты отеля, его фасадов и категорий с разбором по свидетельствам (цепочка надёжности, вклады, судья).
export async function getExplainedFacts(hotelId: number): Promise<ExplainedFact[]> {
  const r = await apiGet<{ facts: ExplainedFact[] }>(`/admin/hotels/${hotelId}/facts`)
  return r.facts
}

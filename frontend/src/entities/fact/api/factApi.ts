import { apiGet } from '@/shared/api'
import type { Characteristic } from '@/shared/api'

let cache: Promise<Characteristic[]> | null = null

// Реестр характеристик: названия и поле значения. Неизменен до перезапуска сервера.
export function getCharacteristics(): Promise<Characteristic[]> {
  if (!cache) {
    cache = apiGet<{ characteristics: Characteristic[] }>('/characteristics').then((r) => r.characteristics)
    cache.catch(() => (cache = null))
  }
  return cache
}

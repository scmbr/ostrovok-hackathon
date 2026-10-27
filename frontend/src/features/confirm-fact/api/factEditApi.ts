import { apiPost } from '@/shared/api'
import type { FactEditOutcome } from '@/shared/api'

// Адрес факта «сущность + характеристика»: id факта меняется при пересборке, адрес — нет.
export interface FactAddress {
  entity_type: string
  entity_id: string
  key: string
}

export const confirmFact = (hotelId: number, a: FactAddress) => apiPost<FactEditOutcome>(`/hotelier/hotels/${hotelId}/facts/confirm`, a)

export const correctFact = (hotelId: number, a: FactAddress, value: string) =>
  apiPost<FactEditOutcome>(`/hotelier/hotels/${hotelId}/facts/correct`, { ...a, value })

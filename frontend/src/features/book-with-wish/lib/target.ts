import type { Category, Desire } from '@/shared/api'
import type { ContractTarget } from '../api/bookingApi'

// Цель контракта и брони для категории: пожелание берётся то, что система предложила для её лучшей характеристики.
export function makeTarget(hotelId: number, cat: Category, wishes?: Desire[]): ContractTarget {
  const w = cat.characteristics.find((c) => c.wish)?.wish
  return {
    hotelId,
    categoryId: cat.id,
    categoryName: cat.name,
    wishes,
    offer: w ? { facadeId: w.facade_id, floorFrom: w.min_floor, text: w.text } : undefined,
  }
}

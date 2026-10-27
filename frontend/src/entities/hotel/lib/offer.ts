import type { HotelCardData } from '@/shared/api'

export interface CategoryOffer {
  facadeId: number
  floorFrom: number
  text: string
  probability?: number
}

// Пожелание к брони, которое система предлагает для категории (лучшее по характеристикам категории); нет — null.
export function categoryOffer(card: HotelCardData, categoryId: number): CategoryOffer | null {
  const cat = card.rooms?.categories.find((c) => c.id === categoryId)
  const w = cat?.characteristics.find((c) => c.wish)?.wish
  return w ? { facadeId: w.facade_id, floorFrom: w.min_floor, text: w.text } : null
}

import { apiGet } from '@/shared/api'
import type { HotelCardData, LookupResult, SimilarHotel } from '@/shared/api'

const cards = new Map<number, Promise<HotelCardData>>()

// Карточка отеля с кешем в памяти: координаты для карты и страница отеля берут одну и ту же.
export function getHotel(id: number, fresh = false): Promise<HotelCardData> {
  if (fresh) cards.delete(id)
  let p = cards.get(id)
  if (!p) {
    p = apiGet<HotelCardData>(`/hotels/${id}`)
    p.catch(() => cards.delete(id))
    cards.set(id, p)
  }
  return p
}

export async function getSimilar(id: number, limit = 5): Promise<SimilarHotel[]> {
  const r = await apiGet<{ similar: SimilarHotel[] }>(`/hotels/${id}/similar?limit=${limit}`)
  return r.similar
}

// Отели, которых гость мог назвать по id или названию (до 5, с оценкой совпадения).
export function lookupHotels(q: string): Promise<LookupResult> {
  return apiGet<LookupResult>(`/hotels/lookup?q=${encodeURIComponent(q)}`)
}

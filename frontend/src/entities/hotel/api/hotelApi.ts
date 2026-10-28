import { apiGet } from '@/shared/api'
import type { HotelCardData, LookupResult, NearbyItem, NearbyResponse, ShowcaseHotel, SimilarHotel } from '@/shared/api'

// Демо-отели для главной: сначала с видом на море, затем с пляжем поблизости.
export const listDemoHotels = () => apiGet<{ hotels: ShowcaseHotel[] }>('/hotels?demo=1').then((r) => r.hotels)

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

const nearby = new Map<string, Promise<NearbyItem[]>>()

// Ближайшее заведение каждого типа (id из словаря: nightclub, bar, pharmacy…); кеш по отелю и набору типов.
export function getNearby(hotelId: number, kinds: string[]): Promise<NearbyItem[]> {
  const key = `${hotelId}:${kinds.join(',')}`
  let p = nearby.get(key)
  if (!p) {
    p = apiGet<NearbyResponse>(`/hotels/${hotelId}/nearby?kinds=${encodeURIComponent(kinds.join(','))}`).then((r) => r.items)
    p.catch(() => nearby.delete(key))
    nearby.set(key, p)
  }
  return p
}

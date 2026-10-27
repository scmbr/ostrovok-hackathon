import { apiGet } from '@/shared/api'
import type { HotelierHotel, Inbox, MissedDemand } from '@/shared/api'

export const getInbox = (hotelId: number) => apiGet<Inbox>(`/hotelier/${hotelId}/inbox`)

export async function getMissedDemand(hotelId: number): Promise<MissedDemand[]> {
  const r = await apiGet<{ missed: MissedDemand[] }>(`/hotelier/${hotelId}/demand`)
  return r.missed
}

export async function getHotelierHotels(): Promise<HotelierHotel[]> {
  const r = await apiGet<{ hotels: HotelierHotel[] }>('/hotelier/hotels')
  return r.hotels
}

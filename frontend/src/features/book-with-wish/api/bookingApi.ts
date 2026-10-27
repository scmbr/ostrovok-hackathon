import { apiGet, apiPost } from '@/shared/api'
import type { Booking, Contract, Desire } from '@/shared/api'
import { getSessionId, touchSession } from '@/entities/session'

// Что бронируем: категория отеля и (необязательно) пожелание из предложенных системой.
export interface ContractTarget {
  hotelId: number
  categoryId: number
  categoryName: string
  wishes?: Desire[]
  // Предложение системы (booking_wish): фасад и этаж. Свободный текст пожелания не разбирается.
  offer?: { facadeId: number; floorFrom: number; text: string; probability?: number }
}

const body = (t: ContractTarget, withWish: boolean) => ({
  session_id: getSessionId(),
  ...(t.wishes?.length ? { wishes: t.wishes } : {}),
  hotel_id: t.hotelId,
  category_id: t.categoryId,
  ...(withWish && t.offer ? { wish: { facade_id: t.offer.facadeId, floor_from: t.offer.floorFrom } } : {}),
})

export function getContract(t: ContractTarget, withWish: boolean): Promise<Contract> {
  touchSession()
  return apiPost<Contract>('/contract', body(t, withWish))
}

export function book(t: ContractTarget, withWish: boolean): Promise<Booking> {
  touchSession()
  return apiPost<Booking>('/bookings', body(t, withWish))
}

export const getBooking = (id: number) => apiGet<Booking>(`/bookings/${id}`)

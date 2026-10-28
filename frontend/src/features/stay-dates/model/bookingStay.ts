import type { Stay } from '@/shared/lib'
import { validStay } from '@/shared/lib'

// API брони даты не принимает: помним выбранные при бронировании даты в браузере, только для показа.
const key = (bookingId: number) => `booking_stay_${bookingId}`

export function saveBookingStay(bookingId: number, s: Stay) {
  try {
    localStorage.setItem(key(bookingId), JSON.stringify({ checkin: s.checkin, checkout: s.checkout }))
  } catch {
    // не критично
  }
}

export function loadBookingStay(bookingId: number): Stay | null {
  try {
    const raw = localStorage.getItem(key(bookingId))
    const v = raw ? (JSON.parse(raw) as { checkin?: string; checkout?: string }) : null
    // заезд мог пройти: для показа брони проверка «не в прошлом» не нужна
    return v ? validStay(v.checkin, v.checkout, null) : null
  } catch {
    return null
  }
}

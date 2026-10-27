import { useSyncExternalStore } from 'react'
import { apiPost } from '@/shared/api'
import type { AskResult } from '@/shared/api'
import { getSessionId, touchSession } from '@/entities/session'

// Какие вопросы отелям уже заданы в этой сессии: пункт переходит в «спросили отель» и не блокирует бронь.
let asked: ReadonlyMap<string, AskResult> = new Map()
const listeners = new Set<() => void>()

const id = (hotelId: number, key: string) => `${hotelId}:${key}`

export async function askHotel(hotelId: number, key: string): Promise<AskResult> {
  touchSession()
  const r = await apiPost<AskResult>('/contract/ask', { session_id: getSessionId(), hotel_id: hotelId, key })
  asked = new Map(asked).set(id(hotelId, key), r)
  listeners.forEach((l) => l())
  return r
}

export function useAsked(hotelId: number, key: string): AskResult | undefined {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    () => asked.get(id(hotelId, key)),
  )
}

import { useSyncExternalStore } from 'react'
import type { Shown } from '@/shared/api'
import { ApiError } from '@/shared/api'
import { getSessionId, onSessionReset, startNewSession, touchSession } from '@/entities/session'
import { addToShortlist, fetchShortlist, removeFromShortlist } from '../api/shortlistApi'

// Шортлист лежит в серверной сессии диалога (TTL 1 час); здесь только его копия для интерфейса.
interface State {
  hotels: Shown[]
  error: string | null
}

let state: State = { hotels: [], error: null }
const listeners = new Set<() => void>()
let loaded = false

function set(next: Partial<State>) {
  state = { ...state, ...next }
  listeners.forEach((l) => l())
}

// Сервер не нашёл сессию или вернул пустой список там, где он был непустым: сессия истекла.
function expired() {
  startNewSession('Сессия истекла, шортлист очищен.')
}

async function run(op: () => Promise<{ hotels: Shown[] }>) {
  try {
    const was = state.hotels.length
    const r = await op()
    touchSession()
    set({ hotels: r.hotels, error: null })
    return was
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      expired()
      return 0
    }
    set({ error: e instanceof Error ? e.message : 'не удалось обновить шортлист' })
    return 0
  }
}

export async function refreshShortlist() {
  const was = state.hotels.length
  await run(() => fetchShortlist(getSessionId()))
  if (was > 0 && state.hotels.length === 0) expired()
  loaded = true
}

export const addHotel = (id: number) => run(() => addToShortlist(getSessionId(), id))
export const removeHotel = (id: number) => run(() => removeFromShortlist(getSessionId(), id))
onSessionReset(() => set({ hotels: [] }))

export function useShortlist(): State {
  const s = useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      if (!loaded) {
        loaded = true
        void refreshShortlist()
      }
      return () => listeners.delete(cb)
    },
    () => state,
  )
  return s
}

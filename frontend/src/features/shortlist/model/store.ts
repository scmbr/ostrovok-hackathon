import { useSyncExternalStore } from 'react'
import type { Shown } from '@/shared/api'
import { onSessionReset, touchSession, withSession } from '@/entities/session'
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

async function run(op: () => Promise<{ hotels: Shown[] }>) {
  try {
    const was = state.hotels.length
    const r = await op()
    touchSession()
    set({ hotels: r.hotels, error: null })
    return was
  } catch (e) {
    set({ error: e instanceof Error ? e.message : 'не удалось обновить шортлист' })
    return 0
  }
}

export async function refreshShortlist() {
  await run(() => withSession(fetchShortlist))
  loaded = true
}

export const addHotel = (id: number) => run(() => withSession((sid) => addToShortlist(sid, id)))
export const removeHotel = (id: number) => run(() => withSession((sid) => removeFromShortlist(sid, id)))
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

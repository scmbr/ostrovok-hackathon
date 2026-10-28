import { useSyncExternalStore } from 'react'

const KEY = 'tima.session_id'
let notice: string | null = null
const listeners = new Set<() => void>()
const resetListeners = new Set<() => void>()

// Подписка на начало новой сессии: хранилища, чьё состояние лежит в серверной сессии (шортлист), сбрасываются.
export function onSessionReset(cb: () => void): void {
  resetListeners.add(cb)
}

function randomId(): string {
  const c = globalThis.crypto
  return c?.randomUUID ? c.randomUUID() : `s-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

function read(): string | null {
  try {
    return localStorage.getItem(KEY)
  } catch {
    return null
  }
}

let memory: string | null = null

export function getSessionId(): string {
  const stored = read()
  if (stored) return stored
  if (!memory) memory = randomId()
  try {
    localStorage.setItem(KEY, memory)
  } catch {
    // без localStorage сессия живёт до перезагрузки страницы
  }
  return memory
}

// Сессия на сервере живёт 1 час. Если сервер её не нашёл, начинаем новую и сообщаем об этом.
export function startNewSession(reason?: string, id?: string): string {
  memory = id ?? randomId()
  try {
    localStorage.setItem(KEY, memory)
  } catch {
    // см. выше
  }
  notice = reason ?? null
  resetListeners.forEach((l) => l())
  listeners.forEach((l) => l())
  return memory
}

export function useSessionNotice(): [string | null, () => void] {
  const n = useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    () => notice,
  )
  return [
    n,
    () => {
      notice = null
      listeners.forEach((l) => l())
    },
  ]
}

export const ACTIVE_KEY = 'tima.session_active_at'
// TTL сессии на сервере — 1 час (dialog_sessions); с запасом считаем её истёкшей раньше.
const TTL_MS = 55 * 60 * 1000

// Отмечает активность: каждый запрос к серверу продлевает сессию.
export function touchSession(): void {
  try {
    localStorage.setItem(ACTIVE_KEY, String(Date.now()))
  } catch {
    // см. выше
  }
}

// При старте приложения: если с последнего запроса прошёл час, сервер сессию уже удалил — начинаем новую.
export function checkSessionExpiry(): boolean {
  try {
    const at = Number(localStorage.getItem(ACTIVE_KEY))
    if (at && Date.now() - at > TTL_MS && read()) {
      startNewSession('Сессия истекла, шортлист очищен.')
      return true
    }
  } catch {
    // см. выше
  }
  return false
}

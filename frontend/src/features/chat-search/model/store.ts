import { useSyncExternalStore } from 'react'
import { postSse } from '@/shared/api'
import type {
  Clarification,
  Compare,
  DialogExample,
  DoneEvent,
  MessageEvent,
  Query,
  LookupHotel,
  ResultsEvent,
  SseEvent,
  StepEvent,
} from '@/shared/api'
import { lookupHotels } from '@/entities/hotel'
import { getSessionId, startNewSession, touchSession } from '@/entities/session'
import { findForeignCity } from '../lib/cities'

export interface UserTurn {
  id: number
  kind: 'user'
  text: string
}

export interface AgentTurn {
  id: number
  kind: 'agent'
  running: boolean
  steps: StepEvent[]
  clarification?: Clarification
  // id кнопки, которой гость ответил на уточнение
  answered?: string
  results?: ResultsEvent
  compare?: Compare
  message?: MessageEvent
  done?: DoneEvent
  error?: string
}

// Ответ системы до агента: «похоже, вы ищете этот отель» или «город вне охвата». Решение гостя — кнопка.
export interface LookupTurn {
  id: number
  kind: 'lookup'
  message: string
  hotels: LookupHotel[]
  resolved: boolean
}

export interface CityTurn {
  id: number
  kind: 'city'
  message: string
  city: string
  resolved: boolean
}

// Гость назвал отель, которого нет в базе.
export interface UnknownHotelTurn {
  id: number
  kind: 'unknown'
  message: string
  resolved: boolean
}

export type Turn = UserTurn | AgentTurn | LookupTurn | CityTurn | UnknownHotelTurn

// Совпадение по названию показываем, если гость назвал id или оценка не ниже порога.
const LOOKUP_THRESHOLD = 0.5

interface State {
  turns: Turn[]
  busy: boolean
  // Демо-диалог из фикстуры, который гость начал кнопкой-примером, и номер следующего хода в нём.
  example?: DialogExample
  cursor: number
}

let state: State = { turns: [], busy: false, cursor: 0 }
const listeners = new Set<() => void>()
let nextId = 1
let controller: AbortController | null = null

function set(next: Partial<State>) {
  state = { ...state, ...next }
  listeners.forEach((l) => l())
}

function patchAgent(id: number, f: (t: AgentTurn) => AgentTurn) {
  set({ turns: state.turns.map((t) => (t.kind === 'agent' && t.id === id ? f(t) : t)) })
}

function applyEvent(id: number, e: SseEvent) {
  patchAgent(id, (t) => {
    switch (e.event) {
      case 'step':
        return { ...t, steps: [...t.steps, e.data as StepEvent] }
      case 'clarification':
        return { ...t, clarification: e.data as Clarification }
      case 'results':
        return { ...t, results: e.data as ResultsEvent }
      case 'compare':
        return { ...t, compare: e.data as Compare }
      case 'message':
        return { ...t, message: e.data as MessageEvent }
      case 'done':
        return { ...t, done: e.data as DoneEvent, running: false }
      case 'error':
        return { ...t, error: (e.data as { error?: string }).error ?? 'ошибка диалога', running: false }
      default:
        return t
    }
  })
}

interface SendArgs {
  message?: string
  choice?: string
  hotelId?: number
  // подпись реплики гостя в ленте (для кнопок — текст кнопки)
  label?: string
  // первый ход демо-диалога: сам пример, курсор не двигаем
  initial?: boolean
  // реплика гостя уже в ленте (перед ней был блок «Похоже, вы ищете» или «город вне охвата»)
  userShown?: boolean
}

export async function send({ message = '', choice = '', hotelId, label, initial, userShown }: SendArgs): Promise<void> {
  if (state.busy) return
  // Продвигаем демо-диалог, только если гость сделал именно тот ход, что записан в фикстуре.
  let { example, cursor } = state
  if (example && !initial) {
    const want = example.next[cursor]
    const sent = choice ? `@${choice}` : message
    if (want !== undefined && want === sent) cursor++
    else example = undefined
  }
  const userId = nextId++
  const agentId = nextId++
  // Уточнение, на которое отвечает кнопка, помечаем отвеченным.
  const turns = state.turns.map((t) => (t.kind === 'agent' && t.clarification && !t.answered && choice ? { ...t, answered: choice } : t))
  set({
    example,
    cursor,
    busy: true,
    turns: [
      ...turns,
      ...(userShown ? [] : [{ id: userId, kind: 'user', text: label ?? message } as UserTurn]),
      { id: agentId, kind: 'agent', running: true, steps: [] },
    ],
  })
  controller = new AbortController()
  try {
    touchSession()
    await postSse(
      '/chat',
      { session_id: getSessionId(), message, ...(choice ? { choice } : {}), ...(hotelId ? { hotel_id: hotelId } : {}) },
      (e) => applyEvent(agentId, e),
      controller.signal,
    )
    patchAgent(agentId, (t) => ({ ...t, running: false }))
  } catch (err) {
    if ((err as Error).name !== 'AbortError') {
      patchAgent(agentId, (t) => ({ ...t, running: false, error: err instanceof Error ? err.message : 'не удалось выполнить запрос' }))
    }
  } finally {
    set({ busy: false })
  }
}

function patchTurn(id: number, f: (t: LookupTurn | CityTurn | UnknownHotelTurn) => LookupTurn | CityTurn | UnknownHotelTurn) {
  set({ turns: state.turns.map((t) => ((t.kind === 'lookup' || t.kind === 'city' || t.kind === 'unknown') && t.id === id ? f(t) : t)) })
}

// Фраза гостя из строки поиска. До агента: сначала не назван ли конкретный отель, затем не чужой ли город,
// затем не назван ли отель, которого нет в базе. Иначе (или если проверка отеля не удалась) — сразу агенту.
export async function submit(message: string): Promise<void> {
  if (state.busy) return
  set({ busy: true })
  let hits: LookupHotel[] = []
  let unknown = ''
  try {
    const r = await lookupHotels(message)
    hits = r.results.filter((h) => h.match === 'id' || h.score >= LOOKUP_THRESHOLD)
    unknown = r.unknown ?? ''
  } catch {
    // эндпоинт недоступен — это не повод не искать
  }
  const city = hits.length ? null : findForeignCity(message)
  const id = nextId++
  let reply: Turn | null = null
  if (hits.length) reply = { id: nextId++, kind: 'lookup', message, hotels: hits, resolved: false }
  else if (city) reply = { id: nextId++, kind: 'city', message, city, resolved: false }
  else if (unknown) reply = { id: nextId++, kind: 'unknown', message, resolved: false }
  if (reply) {
    set({ busy: false, turns: [...state.turns, { id, kind: 'user', text: message }, reply] })
    return
  }
  set({ busy: false })
  return send({ message })
}

// «Нет, искать по пожеланиям» или «Поискать в Барселоне»: та же фраза уходит агенту, реплика гостя уже в ленте.
export function continueToAgent(turnId: number, message: string): Promise<void> {
  patchTurn(turnId, (t) => ({ ...t, resolved: true }))
  return send({ message, userShown: true })
}

// Кнопка-пример: текст отправляется байт в байт, иначе хеш запроса не совпадёт с кешем диалога.
export function startExample(ex: DialogExample): Promise<void> {
  resetChat(false)
  set({ example: ex, cursor: 0 })
  return send({ message: ex.text, initial: true })
}

export function resetChat(newSession = true) {
  controller?.abort()
  if (newSession) {
    startNewSession()
  }
  set({ turns: [], busy: false, example: undefined, cursor: 0 })
}

export function useChat(): State {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    () => state,
  )
}

// Следующий ход демо-диалога: подсказка гостю (фраза или id кнопки уточнения).
export function nextExampleStep(s: State): string | null {
  return s.example && s.cursor < s.example.next.length ? s.example.next[s.cursor] : null
}

// Последняя выдача поиска и её запрос: из неё берутся желания для сравнения, контракта и брони.
export function lastSearch(s: State): { query: Query; hotelIds: number[] } | null {
  for (let i = s.turns.length - 1; i >= 0; i--) {
    const t = s.turns[i]
    if (t.kind === 'agent' && t.results) {
      const r = t.results
      const ids = r.kind === 'similar' ? (r.similar ?? []).map((h) => h.id) : (r.hotels ?? []).map((h) => h.hotel_id)
      return { query: r.query, hotelIds: ids }
    }
  }
  return null
}

import { useSyncExternalStore } from 'react'
import { postSse } from '@/shared/api'
import type {
  Clarification,
  Compare,
  DialogExample,
  DoneEvent,
  MessageEvent,
  Query,
  ResultsEvent,
  SseEvent,
  StepEvent,
} from '@/shared/api'
import { getSessionId, startNewSession, touchSession } from '@/entities/session'

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

export type Turn = UserTurn | AgentTurn

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
}

export async function send({ message = '', choice = '', hotelId, label, initial }: SendArgs): Promise<void> {
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
      { id: userId, kind: 'user', text: label ?? message },
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

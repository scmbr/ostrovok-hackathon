import { ApiError, apiPost } from '@/shared/api'
import { ACTIVE_KEY, getSessionId, startNewSession } from './session'

let renewing: Promise<string> | null = null

// Сервер не нашёл сессию (истекла, сброс БД): заводим новую на сервере и запоминаем её.
// Сообщение «сессия истекла» показываем, только если сессией уже пользовались.
function renewSession(): Promise<string> {
  renewing ??= (async () => {
    let wasActive = false
    try {
      wasActive = !!localStorage.getItem(ACTIVE_KEY)
    } catch {
      // без localStorage сообщения не будет
    }
    const { session_id } = await apiPost<{ session_id: string }>('/sessions', {})
    return startNewSession(wasActive ? 'Сессия истекла, шортлист очищен.' : undefined, session_id)
  })().finally(() => {
    renewing = null
  })
  return renewing
}

// Вызывает op с текущей сессией; на 404 «сессия не найдена» создаёт новую и повторяет вызов один раз.
export async function withSession<T>(op: (sessionId: string) => Promise<T>): Promise<T> {
  const used = getSessionId()
  try {
    return await op(used)
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 404 || e.code !== 'session_not_found') throw e
    // Параллельный запрос мог уже обновить сессию.
    const next = getSessionId() !== used ? getSessionId() : await renewSession()
    return op(next)
  }
}

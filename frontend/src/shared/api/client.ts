export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    // Список замечаний, если сервер вернул `errors` (например, проверка YAML характеристики).
    public details: string[] = [],
  ) {
    super(message)
  }
}

export async function errorText(res: Response, path: string): Promise<string> {
  try {
    const j = (await res.json()) as { error?: string }
    if (j.error) return j.error
  } catch {
    // тело не JSON
  }
  return `API ${path}: ${res.status}`
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    const raw = await res.text()
    let message = `API ${path}: ${res.status}`
    let details: string[] = []
    try {
      const j = JSON.parse(raw) as { error?: string; errors?: string[] }
      if (j.error) message = j.error
      if (Array.isArray(j.errors)) details = j.errors
    } catch {
      // тело не JSON
    }
    throw new ApiError(res.status, message, details)
  }
  return res.json() as Promise<T>
}

export const apiGet = <T>(path: string) => request<T>('GET', path)
export const apiPost = <T>(path: string, body: unknown) => request<T>('POST', path, body)
export const apiDelete = <T>(path: string) => request<T>('DELETE', path)

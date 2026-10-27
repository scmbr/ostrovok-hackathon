export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
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
  if (!res.ok) throw new ApiError(res.status, await errorText(res, path))
  return res.json() as Promise<T>
}

export const apiGet = <T>(path: string) => request<T>('GET', path)
export const apiPost = <T>(path: string, body: unknown) => request<T>('POST', path, body)
export const apiDelete = <T>(path: string) => request<T>('DELETE', path)

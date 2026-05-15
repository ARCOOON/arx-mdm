import { getAccessToken } from './session'

/** Dashboard REST: Bearer JWT from session storage. */
export function dashboardHeaders(): Headers {
  const h = new Headers()
  const token = getAccessToken()
  if (token) {
    h.set('Authorization', `Bearer ${token}`)
  }
  return h
}

export async function dashboardFetch(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  const headers = dashboardHeaders()
  if (init?.headers) {
    const extra = new Headers(init.headers)
    extra.forEach((v, k) => headers.set(k, v))
  }
  return fetch(path, { ...init, headers })
}

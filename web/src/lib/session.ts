const STORAGE_KEY = 'arx_mdm_access_token'

export function getAccessToken(): string {
  try {
    return localStorage.getItem(STORAGE_KEY)?.trim() ?? ''
  } catch {
    return ''
  }
}

export function setAccessToken(token: string): void {
  localStorage.setItem(STORAGE_KEY, token)
}

export function clearAccessToken(): void {
  localStorage.removeItem(STORAGE_KEY)
}

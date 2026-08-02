// Minimal fetch-based API client. Sends the CSRF token as X-CSRF-Token on
// every mutating request. The token comes from /api/login or /api/session
// (rehydrated on page reload and on stale-CSRF 403s).
let csrfToken = ''

/** setCsrfToken stores the token returned by /api/login or /api/session. */
export function setCsrfToken(token: string): void {
  csrfToken = token
}

// Called on any 401 outside /api/login so the app can clear the auth store
// and route to /login. Registered in main.ts to avoid an import cycle
// (stores/auth.ts imports this module).
let onSessionLost: () => void = () => {}

/** setSessionLostHandler registers the global 401 handler. */
export function setSessionLostHandler(handler: () => void): void {
  onSessionLost = handler
}

/** SessionInfo is the GET /api/session response. */
export interface SessionInfo {
  username: string
  typ: string
  groups: string[]
  csrf_token: string
}

/**
 * ApiError carries the HTTP status so callers can branch on it, `key`, an
 * i18n key for a user-facing message, and for 422 validation failures
 * `fields`, the per-field i18n error keys reported by the API.
 */
export class ApiError extends Error {
  status: number
  key: string
  fields?: Record<string, string[]>
  /** Raw response body, for endpoints with custom error payloads (migration wizard). */
  body?: string
  constructor(status: number, message: string, body?: string) {
    super(message)
    this.status = status
    this.body = body
    this.key = status === 403 ? 'error.forbidden' : 'error.request_failed'
    // The API error body is {error: {key, fields?}} (i18n keys).
    try {
      const parsed = JSON.parse(body ?? '') as { error?: { key?: string; fields?: Record<string, string[]> } }
      if (parsed?.error?.key) this.key = parsed.error.key
      if (parsed?.error?.fields) this.fields = parsed.error.fields
    } catch {
      // Non-JSON body: keep the status-derived key.
    }
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  retryOn403 = true,
): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  if (!res.ok) {
    if (res.status === 401 && path !== '/api/login') onSessionLost()
    if (res.status === 403 && retryOn403 && /csrf/i.test(text)) {
      // Stale CSRF token (e.g. server restart): rehydrate once and retry.
      const session = await request<SessionInfo>('GET', '/api/session', undefined, false)
      if (session?.csrf_token) setCsrfToken(session.csrf_token)
      return request<T>(method, path, body, false)
    }
    throw new ApiError(res.status, `${method} ${path}: ${res.status}`, text)
  }
  return (text ? JSON.parse(text) : null) as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),
}

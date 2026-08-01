// Tests for the session bootstrap / CSRF flow: GET /api/session rehydration,
// global 401 handling and the one-shot 403 CSRF retry. fetch is mocked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../auth'
import { api, ApiError, setCsrfToken, setSessionLostHandler } from '../../api'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

function res(status: number, body: unknown = '') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

const session = { username: 'admin', typ: 'admin', groups: ['admin'], csrf_token: 'tok1' }

beforeEach(() => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
  sessionStorage.clear()
  setCsrfToken('')
  setSessionLostHandler(() => {})
})

describe('auth store bootstrap', () => {
  it('rehydrates username/typ and CSRF token from /api/session', async () => {
    fetchMock.mockResolvedValueOnce(res(200, session))
    const auth = useAuthStore()
    await auth.bootstrap()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/session')
    expect(auth.username).toBe('admin')
    expect(auth.typ).toBe('admin')
    expect(auth.isAuthenticated).toBe(true)

    // The rehydrated CSRF token is sent on the next mutating request.
    fetchMock.mockResolvedValueOnce(res(200))
    await api.post('/api/logout')
    expect(fetchMock.mock.calls[1][1].headers['X-CSRF-Token']).toBe('tok1')
  })

  it('clears the store and display cache on 401', async () => {
    sessionStorage.setItem('auth.username', 'stale')
    fetchMock.mockResolvedValueOnce(res(401))
    const auth = useAuthStore()
    await auth.bootstrap()

    expect(auth.username).toBe('')
    expect(auth.isAuthenticated).toBe(false)
    expect(sessionStorage.getItem('auth.username')).toBeNull()
  })
})

describe('auth store login', () => {
  it('stores username and CSRF token on success', async () => {
    fetchMock.mockResolvedValueOnce(res(200, { username: 'admin', csrf_token: 'tok2' }))
    const auth = useAuthStore()
    expect(await auth.login('admin', 'pw', false)).toBe(true)
    expect(auth.username).toBe('admin')
    expect(sessionStorage.getItem('auth.username')).toBe('admin')
  })

  it('sets an i18n error key on failure (no dev mock fallback)', async () => {
    fetchMock.mockResolvedValueOnce(res(404))
    const auth = useAuthStore()
    expect(await auth.login('admin', 'pw', false)).toBe(false)
    expect(auth.username).toBe('')
    expect(auth.error).toBe('login.failed')
  })
})

describe('api client', () => {
  it('invokes the session-lost handler on 401', async () => {
    const handler = vi.fn()
    setSessionLostHandler(handler)
    fetchMock.mockResolvedValueOnce(res(401))
    await expect(api.get('/api/sites')).rejects.toBeInstanceOf(ApiError)
    expect(handler).toHaveBeenCalledOnce()
  })

  it('does not treat a failed login as a lost session', async () => {
    const handler = vi.fn()
    setSessionLostHandler(handler)
    fetchMock.mockResolvedValueOnce(res(401))
    await expect(api.post('/api/login', {})).rejects.toBeInstanceOf(ApiError)
    expect(handler).not.toHaveBeenCalled()
  })

  it('rehydrates the session once and retries on a CSRF 403', async () => {
    setCsrfToken('stale')
    fetchMock
      .mockResolvedValueOnce(res(403, { error: 'CSRF token mismatch' }))
      .mockResolvedValueOnce(res(200, { ...session, csrf_token: 'fresh' }))
      .mockResolvedValueOnce(res(200, { ok: true }))

    const out = await api.post<{ ok: boolean }>('/api/sites', { name: 'x' })
    expect(out.ok).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock.mock.calls[1][0]).toBe('/api/session')
    expect(fetchMock.mock.calls[2][1].headers['X-CSRF-Token']).toBe('fresh')
  })

  it('gives non-CSRF 403s an i18n key and does not retry', async () => {
    fetchMock.mockResolvedValueOnce(res(403, { error: 'not allowed' }))
    const err = await api.delete('/api/sites/1').catch((e: ApiError) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).key).toBe('error.forbidden')
    expect(fetchMock).toHaveBeenCalledOnce()
  })
})

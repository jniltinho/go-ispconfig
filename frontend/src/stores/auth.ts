// Pinia auth store: login/logout against /api/login and /api/logout, and
// bootstrap() against GET /api/session so the session (and CSRF token)
// survives a page reload. /api/session is the source of truth;
// sessionStorage only caches the username for display before bootstrap.
import { defineStore } from 'pinia'
import { api, setCsrfToken, type SessionInfo } from '../api'

interface LoginResponse {
  username?: string
  typ?: string
  csrf_token?: string
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    username: sessionStorage.getItem('auth.username') ?? '',
    typ: '' as string,
    modules: [] as string[],
    error: '' as string,
  }),
  getters: {
    isAuthenticated: (state) => state.username !== '',
  },
  actions: {
    /**
     * bootstrap rehydrates username/typ and the CSRF token from
     * GET /api/session. On 401 (or any failure) the local session is cleared.
     * Runs at app start, before the router's first guard.
     */
    async bootstrap(): Promise<void> {
      try {
        const session = await api.get<SessionInfo>('/api/session')
        this.username = session.username
        this.typ = session.typ
        this.modules = session.modules ?? []
        setCsrfToken(session.csrf_token)
        sessionStorage.setItem('auth.username', this.username)
      } catch {
        this.clear()
      }
    },
    async login(username: string, password: string, stayLoggedIn: boolean): Promise<boolean> {
      this.error = ''
      try {
        const res = await api.post<LoginResponse>('/api/login', {
          username,
          password,
          stay_logged_in: stayLoggedIn,
        })
        if (res?.csrf_token) setCsrfToken(res.csrf_token)
        this.username = res?.username ?? username
        this.typ = res?.typ ?? ''
      } catch {
        this.error = 'login.failed'
        return false
      }
      sessionStorage.setItem('auth.username', this.username)
      // Module visibility comes from GET /api/session (the login response
      // carries no modules); fetch it for the top nav, non-fatally.
      try {
        const session = await api.get<SessionInfo>('/api/session')
        this.modules = session.modules ?? []
      } catch {
        this.modules = []
      }
      return true
    },
    async logout(): Promise<void> {
      try {
        await api.post('/api/logout')
      } catch {
        // Clear the local session regardless.
      }
      this.clear()
    },
    /** clear drops the local session: store state, CSRF token, display cache. */
    clear(): void {
      setCsrfToken('')
      this.username = ''
      this.typ = ''
      this.modules = []
      sessionStorage.removeItem('auth.username')
    },
  },
})

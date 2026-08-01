// Pinia auth store: login/logout against /api/login and /api/logout.
// The endpoints land with the auth group (group 6); until then a 404 in dev
// mode logs in with a mock session so the layout can be developed.
import { defineStore } from 'pinia'
import { api, ApiError, setCsrfToken } from '../api'

interface LoginResponse {
  username?: string
  csrf_token?: string
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    username: sessionStorage.getItem('auth.username') ?? '',
    error: '' as string,
  }),
  getters: {
    isAuthenticated: (state) => state.username !== '',
  },
  actions: {
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
      } catch (err) {
        if (err instanceof ApiError && err.status === 404 && import.meta.env.DEV) {
          // Backend auth endpoints not implemented yet (group 6).
          console.warn('[auth] /api/login returned 404 — using mock session (dev only)')
          this.username = username
        } else {
          this.error = 'login.failed'
          return false
        }
      }
      sessionStorage.setItem('auth.username', this.username)
      return true
    },
    async logout(): Promise<void> {
      try {
        await api.post('/api/logout')
      } catch {
        // Endpoint may not exist yet; clear the local session regardless.
      }
      setCsrfToken('')
      this.username = ''
      sessionStorage.removeItem('auth.username')
    },
  },
})

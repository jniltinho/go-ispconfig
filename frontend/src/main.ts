// Frontend entrypoint: Vue 3 + Pinia + vue-router, styles with the
// ISPConfig3-derived theme tokens.
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { setSessionLostHandler } from './api'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
app.use(createPinia())

const auth = useAuthStore()

// Any API call that comes back 401 clears the session and lands on /login.
setSessionLostHandler(() => {
  auth.clear()
  void router.push({ name: 'login' })
})

// Rehydrate the session (username/typ + CSRF token) before the router's
// first guard runs, so an F5 keeps the user logged in — or, on 401, the
// guard sends them to /login with a clean store.
void auth.bootstrap().finally(() => {
  app.use(router)
  app.mount('#app')
})

// Router: /login is public; every module route renders inside AppShell and
// requires an authenticated session.
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'
import AppShell from './components/AppShell.vue'
import LoginView from './views/LoginView.vue'
import DashboardView from './views/DashboardView.vue'
import ModulePlaceholder from './views/ModulePlaceholder.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', name: 'login', component: LoginView },
    {
      path: '/',
      component: AppShell,
      children: [
        { path: 'dashboard', name: 'dashboard', component: DashboardView },
        { path: 'sites', name: 'sites', component: ModulePlaceholder },
        { path: 'sites/folders', name: 'sites-folders', component: ModulePlaceholder },
        { path: 'dns', name: 'dns', component: ModulePlaceholder },
        { path: 'system', name: 'system', component: ModulePlaceholder },
      ],
    },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.name !== 'login' && !auth.isAuthenticated) return { name: 'login' }
  if (to.name === 'login' && auth.isAuthenticated) return { name: 'dashboard' }
})

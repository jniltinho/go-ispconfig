// Router: /login is public; every module route renders inside AppShell and
// requires an authenticated session.
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'
import AppShell from './components/AppShell.vue'
import LoginView from './views/LoginView.vue'
import DashboardView from './views/DashboardView.vue'
import ModulePlaceholder from './views/ModulePlaceholder.vue'
import WebDomainList from './views/sites/WebDomainList.vue'
import WebFolderList from './views/sites/WebFolderList.vue'
import WebFolderUserList from './views/sites/WebFolderUserList.vue'
import EntityForm from './views/sites/EntityForm.vue'

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
        { path: 'sites', name: 'sites', component: WebDomainList },
        {
          path: 'sites/domains/new',
          name: 'sites-domain-new',
          component: EntityForm,
          props: { entity: 'web-domains', apiBase: '/api/sites/web-domains', backTo: '/sites' },
        },
        {
          path: 'sites/domains/:id',
          name: 'sites-domain-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'web-domains',
            apiBase: '/api/sites/web-domains',
            backTo: '/sites',
            id: String(route.params.id),
          }),
        },
        { path: 'sites/folders', name: 'sites-folders', component: WebFolderList },
        {
          path: 'sites/folders/new',
          name: 'sites-folder-new',
          component: EntityForm,
          props: {
            entity: 'web-folders',
            apiBase: '/api/sites/web-folders',
            backTo: '/sites/folders',
          },
        },
        {
          path: 'sites/folders/:id',
          name: 'sites-folder-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'web-folders',
            apiBase: '/api/sites/web-folders',
            backTo: '/sites/folders',
            id: String(route.params.id),
          }),
        },
        {
          path: 'sites/folders/:folderId/users',
          name: 'sites-folder-users',
          component: WebFolderUserList,
          props: (route) => ({ folderId: String(route.params.folderId) }),
        },
        {
          path: 'sites/folders/:folderId/users/new',
          name: 'sites-folder-user-new',
          component: EntityForm,
          props: (route) => ({
            entity: 'web-folder-users',
            apiBase: '/api/sites/web-folder-users',
            backTo: `/sites/folders/${route.params.folderId}/users`,
            fixed: { web_folder_id: Number(route.params.folderId) },
          }),
        },
        {
          path: 'sites/folders/:folderId/users/:id',
          name: 'sites-folder-user-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'web-folder-users',
            apiBase: '/api/sites/web-folder-users',
            backTo: `/sites/folders/${route.params.folderId}/users`,
            id: String(route.params.id),
          }),
        },
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

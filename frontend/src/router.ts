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
import ZoneList from './views/dns/ZoneList.vue'
import ZoneForm from './views/dns/ZoneForm.vue'
import ZoneWizard from './views/dns/ZoneWizard.vue'
import SlaveZoneList from './views/dns/SlaveZoneList.vue'
import TemplateList from './views/dns/TemplateList.vue'
import MigrationWizard from './views/system/MigrationWizard.vue'
import ClientList from './views/clients/ClientList.vue'
import ResellerList from './views/clients/ResellerList.vue'
import ClientForm from './views/clients/ClientForm.vue'
import LimitTemplateList from './views/clients/LimitTemplateList.vue'

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
        { path: 'clients', name: 'clients', component: ClientList },
        { path: 'clients/resellers', name: 'resellers', component: ResellerList },
        { path: 'clients/new', name: 'client-new', component: ClientForm },
        { path: 'clients/resellers/new', name: 'reseller-new', component: ClientForm, props: { reseller: true } },
        {
          path: 'clients/resellers/:id(\\d+)',
          name: 'reseller-edit',
          component: ClientForm,
          props: (route) => ({ id: String(route.params.id), reseller: true }),
        },
        { path: 'clients/limit-templates', name: 'limit-templates', component: LimitTemplateList },
        {
          path: 'clients/limit-templates/new',
          name: 'limit-template-new',
          component: EntityForm,
          props: { entity: 'client-templates', apiBase: '/api/client-templates', backTo: '/clients/limit-templates' },
        },
        {
          path: 'clients/limit-templates/:id',
          name: 'limit-template-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'client-templates',
            apiBase: '/api/client-templates',
            backTo: '/clients/limit-templates',
            id: String(route.params.id),
          }),
        },
        {
          path: 'clients/:id(\\d+)',
          name: 'client-edit',
          component: ClientForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
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
        { path: 'dns', name: 'dns', component: ZoneList },
        { path: 'dns/wizard', name: 'dns-wizard', component: ZoneWizard },
        {
          path: 'dns/zones/new',
          name: 'dns-zone-new',
          component: EntityForm,
          props: { entity: 'zones', apiBase: '/api/dns/zones', backTo: '/dns' },
        },
        {
          path: 'dns/zones/:id',
          name: 'dns-zone-edit',
          component: ZoneForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
        { path: 'dns/slave-zones', name: 'dns-slave-zones', component: SlaveZoneList },
        {
          path: 'dns/slave-zones/new',
          name: 'dns-slave-zone-new',
          component: EntityForm,
          props: {
            entity: 'slave-zones',
            apiBase: '/api/dns/slave-zones',
            backTo: '/dns/slave-zones',
          },
        },
        {
          path: 'dns/slave-zones/:id',
          name: 'dns-slave-zone-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'slave-zones',
            apiBase: '/api/dns/slave-zones',
            backTo: '/dns/slave-zones',
            id: String(route.params.id),
          }),
        },
        { path: 'dns/templates', name: 'dns-templates', component: TemplateList },
        {
          path: 'dns/templates/new',
          name: 'dns-template-new',
          component: EntityForm,
          props: {
            entity: 'zone-templates',
            apiBase: '/api/dns/zone-templates',
            backTo: '/dns/templates',
          },
        },
        {
          path: 'dns/templates/:id',
          name: 'dns-template-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'zone-templates',
            apiBase: '/api/dns/zone-templates',
            backTo: '/dns/templates',
            id: String(route.params.id),
          }),
        },
        { path: 'system', name: 'system', component: ModulePlaceholder },
        {
          path: 'system/migration',
          name: 'system-migration',
          component: MigrationWizard,
          meta: { adminOnly: true },
        },
      ],
    },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.name !== 'login' && !auth.isAuthenticated) return { name: 'login' }
  if (to.name === 'login' && auth.isAuthenticated) return { name: 'dashboard' }
  // Admin-only routes (the API enforces this too; the guard avoids
  // rendering a view that can only 403).
  if (to.meta.adminOnly && auth.typ !== 'admin') return { name: 'dashboard' }
})

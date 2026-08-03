// Router: /login is public; every module route renders inside AppShell and
// requires an authenticated session. Soft route-loading indicator is driven
// by Pinia useUiStore (progress bar + content spinner in AppShell).
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'
import { useUiStore } from './stores/ui'
import AppShell from './components/AppShell.vue'
import LoginView from './views/LoginView.vue'
import DashboardView from './views/DashboardView.vue'
import ModulePlaceholder from './views/ModulePlaceholder.vue'
import WebDomainList from './views/sites/WebDomainList.vue'
import WebFolderList from './views/sites/WebFolderList.vue'
import WebFolderUserList from './views/sites/WebFolderUserList.vue'
import DatabaseList from './views/sites/DatabaseList.vue'
import DatabaseForm from './views/sites/DatabaseForm.vue'
import DatabaseUserForm from './views/sites/DatabaseUserForm.vue'
import DatabaseUserList from './views/sites/DatabaseUserList.vue'
import CronList from './views/sites/CronList.vue'
import CronForm from './views/sites/CronForm.vue'
import ShellUserForm from './views/sites/ShellUserForm.vue'
import ShellUserList from './views/sites/ShellUserList.vue'
import FTPUserForm from './views/sites/FTPUserForm.vue'
import FTPUserList from './views/sites/FTPUserList.vue'
import EntityForm from './views/sites/EntityForm.vue'
import WebDomainForm from './views/sites/WebDomainForm.vue'
import MailList from './views/mail/MailList.vue'
import DomainForm from './views/mail/DomainForm.vue'
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
import MessageTemplateList from './views/clients/MessageTemplateList.vue'
import SendMessageView from './views/clients/SendMessageView.vue'

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
        {
          path: 'mail',
          name: 'mail-domains',
          component: MailList,
          props: {
            apiBase: '/api/mail/domains', idField: 'domain_id', formBase: '/mail/domains',
            columns: [
              { key: 'active', label: 'Active' },
              { key: '_server_name', label: 'Server' },
              { key: 'domain', label: 'Domain' },
            ],
            titleKey: 'mail.domains_title', addKey: 'mail.add_domain',
          },
        },
        { path: 'mail/domains/new', name: 'mail-domain-new', component: DomainForm },
        {
          path: 'mail/domains/:id(\\d+)',
          name: 'mail-domain-edit',
          component: DomainForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
        {
          path: 'mail/mailboxes',
          name: 'mail-mailboxes',
          component: MailList,
          props: {
            apiBase: '/api/mail/mailboxes', idField: 'mailuser_id', formBase: '/mail/mailboxes',
            columns: [
              { key: 'email', label: 'Email' },
              { key: 'name', label: 'Name' },
              { key: 'quota', label: 'Quota' },
            ],
            titleKey: 'mail.mailboxes_title', addKey: 'mail.add_mailbox',
          },
        },
        {
          path: 'mail/mailboxes/new',
          name: 'mail-mailbox-new',
          component: EntityForm,
          props: { entity: 'mailboxes', apiBase: '/api/mail/mailboxes', backTo: '/mail/mailboxes' },
        },
        {
          path: 'mail/mailboxes/:id(\\d+)',
          name: 'mail-mailbox-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'mailboxes', apiBase: '/api/mail/mailboxes', backTo: '/mail/mailboxes', id: String(route.params.id) }),
        },
        {
          path: 'mail/aliases',
          name: 'mail-aliases',
          component: MailList,
          props: {
            apiBase: '/api/mail/aliases', idField: 'forwarding_id', formBase: '/mail/aliases',
            columns: [{ key: 'source', label: 'Source' }, { key: 'destination', label: 'Destination' }],
            titleKey: 'mail.aliases_title', addKey: 'mail.add_alias',
          },
        },
        {
          path: 'mail/aliases/new',
          name: 'mail-aliases-new',
          component: EntityForm,
          props: { entity: 'aliases', apiBase: '/api/mail/aliases', backTo: '/mail/aliases' },
        },
        {
          path: 'mail/aliases/:id(\\d+)',
          name: 'mail-aliases-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'aliases', apiBase: '/api/mail/aliases', backTo: '/mail/aliases', id: String(route.params.id) }),
        },
        {
          path: 'mail/forwards',
          name: 'mail-forwards',
          component: MailList,
          props: {
            apiBase: '/api/mail/forwards', idField: 'forwarding_id', formBase: '/mail/forwards',
            columns: [{ key: 'source', label: 'Source' }, { key: 'destination', label: 'Destination' }],
            titleKey: 'mail.forwards_title', addKey: 'mail.add_forward',
          },
        },
        {
          path: 'mail/forwards/new',
          name: 'mail-forwards-new',
          component: EntityForm,
          props: { entity: 'forwards', apiBase: '/api/mail/forwards', backTo: '/mail/forwards' },
        },
        {
          path: 'mail/forwards/:id(\\d+)',
          name: 'mail-forwards-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'forwards', apiBase: '/api/mail/forwards', backTo: '/mail/forwards', id: String(route.params.id) }),
        },
        {
          path: 'mail/catchalls',
          name: 'mail-catchalls',
          component: MailList,
          props: {
            apiBase: '/api/mail/catchalls', idField: 'forwarding_id', formBase: '/mail/catchalls',
            columns: [{ key: 'source', label: 'Domain' }, { key: 'destination', label: 'Destination' }],
            titleKey: 'mail.catchalls_title', addKey: 'mail.add_catchall',
          },
        },
        {
          path: 'mail/catchalls/new',
          name: 'mail-catchalls-new',
          component: EntityForm,
          props: { entity: 'catchalls', apiBase: '/api/mail/catchalls', backTo: '/mail/catchalls' },
        },
        {
          path: 'mail/catchalls/:id(\\d+)',
          name: 'mail-catchalls-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'catchalls', apiBase: '/api/mail/catchalls', backTo: '/mail/catchalls', id: String(route.params.id) }),
        },
        {
          path: 'mail/alias-domains',
          name: 'mail-alias-domains',
          component: MailList,
          props: {
            apiBase: '/api/mail/alias-domains', idField: 'forwarding_id', formBase: '/mail/alias-domains',
            columns: [{ key: 'source', label: 'Source domain' }, { key: 'destination', label: 'Target domain' }],
            titleKey: 'mail.alias_domains_title', addKey: 'mail.add_alias_domain',
          },
        },
        {
          path: 'mail/alias-domains/new',
          name: 'mail-alias-domains-new',
          component: EntityForm,
          props: { entity: 'alias-domains', apiBase: '/api/mail/alias-domains', backTo: '/mail/alias-domains' },
        },
        {
          path: 'mail/alias-domains/:id(\\d+)',
          name: 'mail-alias-domains-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'alias-domains', apiBase: '/api/mail/alias-domains', backTo: '/mail/alias-domains', id: String(route.params.id) }),
        },
        {
          path: 'mail/transports',
          name: 'mail-transports',
          component: MailList,
          props: {
            apiBase: '/api/mail/transports', idField: 'transport_id', formBase: '/mail/transports',
            columns: [{ key: 'domain', label: 'Domain' }, { key: 'transport', label: 'Transport' }, { key: 'sort_order', label: 'Order' }],
            titleKey: 'mail.transports_title', addKey: 'mail.add_transport',
          },
        },
        {
          path: 'mail/transports/new',
          name: 'mail-transports-new',
          component: EntityForm,
          props: { entity: 'transports', apiBase: '/api/mail/transports', backTo: '/mail/transports' },
        },
        {
          path: 'mail/transports/:id(\\d+)',
          name: 'mail-transports-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'transports', apiBase: '/api/mail/transports', backTo: '/mail/transports', id: String(route.params.id) }),
        },
        {
          path: 'mail/spamfilter/policies',
          name: 'mail-spam-policies',
          component: MailList,
          props: {
            apiBase: '/api/mail/spamfilter/policies', idField: 'id', formBase: '/mail/spamfilter/policies',
            columns: [{ key: 'policy_name', label: 'Name' }, { key: 'rspamd_greylisting', label: 'Greylisting' }],
            titleKey: 'mail.policies_title', addKey: 'mail.add_policy',
          },
        },
        {
          path: 'mail/spamfilter/policies/new',
          name: 'mail-spam-policies-new',
          component: EntityForm,
          props: { entity: 'policies', apiBase: '/api/mail/spamfilter/policies', backTo: '/mail/spamfilter/policies' },
        },
        {
          path: 'mail/spamfilter/policies/:id(\\d+)',
          name: 'mail-spam-policies-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'policies', apiBase: '/api/mail/spamfilter/policies', backTo: '/mail/spamfilter/policies', id: String(route.params.id) }),
        },
        {
          path: 'mail/spamfilter/users',
          name: 'mail-spam-users',
          component: MailList,
          props: {
            apiBase: '/api/mail/spamfilter/users', idField: 'id', formBase: '/mail/spamfilter/users',
            columns: [{ key: 'email', label: 'Email' }, { key: 'priority', label: 'Priority' }],
            titleKey: 'mail.spamusers_title', addKey: 'mail.add_spamuser',
          },
        },
        {
          path: 'mail/spamfilter/users/new',
          name: 'mail-spam-users-new',
          component: EntityForm,
          props: { entity: 'users', apiBase: '/api/mail/spamfilter/users', backTo: '/mail/spamfilter/users' },
        },
        {
          path: 'mail/spamfilter/users/:id(\\d+)',
          name: 'mail-spam-users-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'users', apiBase: '/api/mail/spamfilter/users', backTo: '/mail/spamfilter/users', id: String(route.params.id) }),
        },
        {
          path: 'mail/spamfilter/wblists',
          name: 'mail-spam-wblists',
          component: MailList,
          props: {
            apiBase: '/api/mail/spamfilter/wblists', idField: 'wblist_id', formBase: '/mail/spamfilter/wblists',
            columns: [{ key: 'wb', label: 'List' }, { key: 'email', label: 'Email' }],
            titleKey: 'mail.wblists_title', addKey: 'mail.add_wblist',
          },
        },
        {
          path: 'mail/spamfilter/wblists/new',
          name: 'mail-spam-wblists-new',
          component: EntityForm,
          props: { entity: 'wblists', apiBase: '/api/mail/spamfilter/wblists', backTo: '/mail/spamfilter/wblists' },
        },
        {
          path: 'mail/spamfilter/wblists/:id(\\d+)',
          name: 'mail-spam-wblists-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'wblists', apiBase: '/api/mail/spamfilter/wblists', backTo: '/mail/spamfilter/wblists', id: String(route.params.id) }),
        },
        {
          path: 'mail/access',
          name: 'mail-access',
          component: MailList,
          props: {
            apiBase: '/api/mail/access', idField: 'access_id', formBase: '/mail/access',
            columns: [{ key: 'source', label: 'Source' }, { key: 'type', label: 'Type' }, { key: 'access', label: 'Access' }],
            titleKey: 'mail.access_title', addKey: 'mail.add_access',
          },
        },
        {
          path: 'mail/access/new',
          name: 'mail-access-new',
          component: EntityForm,
          props: { entity: 'access', apiBase: '/api/mail/access', backTo: '/mail/access' },
        },
        {
          path: 'mail/access/:id(\\d+)',
          name: 'mail-access-edit',
          component: EntityForm,
          props: (route) => ({ entity: 'access', apiBase: '/api/mail/access', backTo: '/mail/access', id: String(route.params.id) }),
        },
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
        { path: 'clients/message-templates', name: 'message-templates', component: MessageTemplateList },
        {
          path: 'clients/message-templates/new',
          name: 'message-template-new',
          component: EntityForm,
          props: {
            entity: 'client-message-templates',
            apiBase: '/api/client-message-templates',
            backTo: '/clients/message-templates',
          },
        },
        {
          path: 'clients/message-templates/:id',
          name: 'message-template-edit',
          component: EntityForm,
          props: (route) => ({
            entity: 'client-message-templates',
            apiBase: '/api/client-message-templates',
            backTo: '/clients/message-templates',
            id: String(route.params.id),
          }),
        },
        { path: 'clients/send-message', name: 'send-message', component: SendMessageView },
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
          component: WebDomainForm,
        },
        {
          path: 'sites/domains/:id',
          name: 'sites-domain-edit',
          component: WebDomainForm,
          props: (route) => ({ id: String(route.params.id) }),
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
        { path: 'sites/crons', name: 'sites-crons', component: CronList },
        { path: 'sites/crons/new', name: 'sites-cron-new', component: CronForm },
        {
          path: 'sites/crons/:id',
          name: 'sites-cron-edit',
          component: CronForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
                { path: 'sites/ftp-users', name: 'sites-ftp-users', component: FTPUserList },
        {
          path: 'sites/ftp-users/new',
          name: 'sites-ftp-user-new',
          component: FTPUserForm,
        },
        {
          path: 'sites/ftp-users/:id',
          name: 'sites-ftp-user-edit',
          component: FTPUserForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
        { path: 'sites/shell-users', name: 'sites-shell-users', component: ShellUserList },
        {
          path: 'sites/shell-users/new',
          name: 'sites-shell-user-new',
          component: ShellUserForm,
        },
        {
          path: 'sites/shell-users/:id',
          name: 'sites-shell-user-edit',
          component: ShellUserForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
{ path: 'sites/databases', name: 'sites-databases', component: DatabaseList },
        {
          path: 'sites/databases/new',
          name: 'sites-database-new',
          component: DatabaseForm,
        },
        {
          path: 'sites/databases/:id',
          name: 'sites-database-edit',
          component: DatabaseForm,
          props: (route) => ({ id: String(route.params.id) }),
        },
        { path: 'sites/database-users', name: 'sites-database-users', component: DatabaseUserList },
        {
          path: 'sites/database-users/new',
          name: 'sites-database-user-new',
          component: DatabaseUserForm,
        },
        {
          path: 'sites/database-users/:id',
          name: 'sites-database-user-edit',
          component: DatabaseUserForm,
          props: (route) => ({ id: String(route.params.id) }),
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
          path: 'system/firewall',
          name: 'system-firewall',
          component: MailList,
          meta: { adminOnly: true },
          props: {
            apiBase: '/api/firewall', idField: 'firewall_id', formBase: '/system/firewall',
            columns: [
              { key: 'active', label: 'firewall.col.active' },
              { key: '_server_name', label: 'firewall.col.server' },
              { key: 'tcp_port', label: 'firewall.col.tcp_port' },
              { key: 'udp_port', label: 'firewall.col.udp_port' },
            ],
            titleKey: 'firewall.firewalls_title', addKey: 'firewall.add_firewall',
          },
        },
        {
          path: 'system/firewall/new',
          name: 'system-firewall-new',
          component: EntityForm,
          meta: { adminOnly: true },
          props: { entity: 'firewall', apiBase: '/api/firewall', backTo: '/system/firewall' },
        },
        {
          path: 'system/firewall/:id(\\d+)',
          name: 'system-firewall-edit',
          component: EntityForm,
          meta: { adminOnly: true },
          props: (route) => ({ entity: 'firewall', apiBase: '/api/firewall', backTo: '/system/firewall', id: String(route.params.id), readonlyFields: ['server_id'] }),
        },
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

router.beforeEach((to, from) => {
  const auth = useAuthStore()
  if (to.name !== 'login' && !auth.isAuthenticated) return { name: 'login' }
  if (to.name === 'login' && auth.isAuthenticated) return { name: 'dashboard' }
  // Admin-only routes (the API enforces this too; the guard avoids
  // rendering a view that can only 403).
  if (to.meta.adminOnly && auth.typ !== 'admin') return { name: 'dashboard' }

  // Soft loader: skip initial mount (no matched from) and same-route
  // trivial navigations (identical fullPath). Auth redirects above return
  // early so they never start the loader for the aborted hop.
  if (from.matched.length > 0 && to.fullPath !== from.fullPath) {
    useUiStore().startRouteLoading()
  }
})

router.afterEach(() => {
  useUiStore().stopRouteLoading()
})

router.onError(() => {
  useUiStore().stopRouteLoading()
})

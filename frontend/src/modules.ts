// Enabled modules shown in the topbar plus their sidebar sections.
// Placeholder wiring — real section routes arrive with each module's change.
export interface ModuleDef {
  id: string
  path: string
  sections: { labelKey: string; path: string; adminOnly?: boolean }[]
}

export const modules: ModuleDef[] = [
  {
    id: 'dashboard',
    path: '/dashboard',
    sections: [{ labelKey: 'sidebar.dashboard.overview', path: '/dashboard' }],
  },
  {
    id: 'sites',
    path: '/sites',
    sections: [
      { labelKey: 'sidebar.sites.websites', path: '/sites' },
      { labelKey: 'sidebar.sites.folders', path: '/sites/folders' },
      { labelKey: 'sidebar.sites.databases', path: '/sites/databases' },
      { labelKey: 'sidebar.sites.database_users', path: '/sites/database-users' },
    ],
  },
  {
    id: 'dns',
    path: '/dns',
    sections: [
      { labelKey: 'sidebar.dns.zones', path: '/dns' },
      { labelKey: 'sidebar.dns.slave_zones', path: '/dns/slave-zones' },
      { labelKey: 'sidebar.dns.templates', path: '/dns/templates', adminOnly: true },
    ],
  },
  {
    id: 'mail',
    path: '/mail',
    sections: [
      { labelKey: 'sidebar.mail.domains', path: '/mail' },
      { labelKey: 'sidebar.mail.mailboxes', path: '/mail/mailboxes' },
      { labelKey: 'sidebar.mail.aliases', path: '/mail/aliases' },
      { labelKey: 'sidebar.mail.forwards', path: '/mail/forwards' },
      { labelKey: 'sidebar.mail.catchalls', path: '/mail/catchalls' },
      { labelKey: 'sidebar.mail.alias_domains', path: '/mail/alias-domains' },
      { labelKey: 'sidebar.mail.transports', path: '/mail/transports' },
      { labelKey: 'sidebar.mail.spamfilter_policies', path: '/mail/spamfilter/policies', adminOnly: true },
      { labelKey: 'sidebar.mail.spamfilter_users', path: '/mail/spamfilter/users' },
      { labelKey: 'sidebar.mail.spamfilter_wblist', path: '/mail/spamfilter/wblists' },
      { labelKey: 'sidebar.mail.access', path: '/mail/access' },
    ],
  },
  {
    id: 'client',
    path: '/clients',
    sections: [
      { labelKey: 'sidebar.client.clients', path: '/clients' },
      { labelKey: 'sidebar.client.resellers', path: '/clients/resellers', adminOnly: true },
      { labelKey: 'sidebar.client.limit_templates', path: '/clients/limit-templates', adminOnly: true },
      { labelKey: 'sidebar.client.message_templates', path: '/clients/message-templates' },
      { labelKey: 'sidebar.client.send_message', path: '/clients/send-message' },
    ],
  },
  {
    id: 'system',
    path: '/system',
    sections: [
      { labelKey: 'sidebar.system.server_config', path: '/system' },
      { labelKey: 'sidebar.system.users', path: '/system' },
      { labelKey: 'sidebar.system.firewall', path: '/system/firewall', adminOnly: true },
      { labelKey: 'sidebar.system.migration', path: '/system/migration', adminOnly: true },
    ],
  },
]

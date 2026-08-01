// Enabled modules shown in the topbar plus their sidebar sections.
// Placeholder wiring — real section routes arrive with each module's change.
export interface ModuleDef {
  id: string
  path: string
  sections: { labelKey: string; path: string }[]
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
      { labelKey: 'sidebar.sites.subdomains', path: '/sites' },
      { labelKey: 'sidebar.sites.aliasdomains', path: '/sites' },
    ],
  },
  {
    id: 'dns',
    path: '/dns',
    sections: [
      { labelKey: 'sidebar.dns.zones', path: '/dns' },
      { labelKey: 'sidebar.dns.records', path: '/dns' },
    ],
  },
  {
    id: 'system',
    path: '/system',
    sections: [
      { labelKey: 'sidebar.system.server_config', path: '/system' },
      { labelKey: 'sidebar.system.users', path: '/system' },
    ],
  },
]

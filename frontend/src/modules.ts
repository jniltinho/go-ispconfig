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
    id: 'system',
    path: '/system',
    sections: [
      { labelKey: 'sidebar.system.server_config', path: '/system' },
      { labelKey: 'sidebar.system.users', path: '/system' },
      { labelKey: 'sidebar.system.migration', path: '/system/migration', adminOnly: true },
    ],
  },
]

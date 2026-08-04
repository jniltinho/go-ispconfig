// Lucide icon map per ISPConfig module — 1:1 replacement of the legacy
// `ispconfig` icon font (docs/research/ispconfig3-theme.md).
import {
  Activity,
  BarChart3,
  Calendar,
  CircleHelp,
  ExternalLink,
  Filter,
  Globe,
  LayoutDashboard,
  LogIn,
  Mail,
  Network,
  Pencil,
  Search,
  Settings,
  Trash2,
  Users,
  Wrench,
  type LucideProps,
  X,
} from 'lucide-vue-next'
import type { FunctionalComponent } from 'vue'

export type ModuleIcon = FunctionalComponent<LucideProps>

export const moduleIcons: Record<string, ModuleIcon> = {
  dashboard: LayoutDashboard,
  sites: Globe,
  dns: Network,
  mail: Mail,
  client: Users,
  monitor: Activity,
  system: Settings,
  tools: Wrench,
  help: CircleHelp,
}

// Utility icons: 1:1 replacement of the legacy icon-font action glyphs
// (lens→Search, filter→Filter, edit→Pencil, delete→Trash2,
// link→ExternalLink, stats→BarChart3, loginas→LogIn, calendar→Calendar).
export const utilityIcons = {
  search: Search,
  filter: Filter,
  edit: Pencil,
  delete: Trash2,
  externalLink: ExternalLink,
  stats: BarChart3,
  loginAs: LogIn,
  calendar: Calendar,
  close: X,
} satisfies Record<string, ModuleIcon>

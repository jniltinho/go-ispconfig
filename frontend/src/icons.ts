// Lucide icon map per ISPConfig module — 1:1 replacement of the legacy
// `ispconfig` icon font (docs/research/ispconfig3-theme.md).
import {
  Activity,
  CircleHelp,
  Globe,
  LayoutDashboard,
  Mail,
  Network,
  Settings,
  Users,
  Wrench,
  type LucideProps,
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

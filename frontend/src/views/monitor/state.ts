// Shared monitor helpers: state colouring and the check-type catalogue used
// by the overview and the detail views.

/** ServerState mirrors monitor.ServerState from GET /api/monitor/state. */
export interface ServerState {
  server_id: number
  server_name: string
  state: string
  os_name?: string
  os_version?: string
  ispc_name?: string
  ispc_version?: string
  messages: { severity: string; type: string; text: string }[]
  counts: Record<string, number>
  checks: { type: string; state: string; created: number }[]
}

/** MonitorDataItem mirrors api.MonitorDataItem from GET /api/monitor/data. */
export interface MonitorDataItem {
  server_id: number
  type: string
  created: number
  state: string
  data?: unknown
  data_raw?: string
  decode_error?: string
}

/**
 * stateClass maps a monitor severity to the panel's badge classes. Unknown
 * labels fall back to the neutral surface, so a new PHP severity never
 * renders an unstyled badge.
 */
export function stateClass(state: string): string {
  switch (state) {
    case 'ok':
      return 'border-success bg-success text-white'
    case 'info':
      return 'border-info-border bg-info text-info-text'
    case 'warning':
    case 'critical':
      return 'border-warning-border bg-warning text-warning-text'
    case 'error':
      return 'border-danger-border bg-danger text-danger-text'
    default:
      return 'border-border bg-surface text-text-muted'
  }
}

/** checkTypes are the detail views reachable from the overview, in PHP order. */
export const checkTypes = [
  'cpu_info',
  'mem_usage',
  'disk_usage',
  'server_load',
  'services',
  'os_info',
  'kernel_info',
  'ispc_info',
  'system_update',
  'sys_usage',
  'log_ispconfig',
  'log_letsencrypt',
  'log_messages',
]

/** formatTs renders a unix timestamp in the browser locale, '-' when unset. */
export function formatTs(ts?: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

<script setup lang="ts">
// Web domain form: metadata-driven EntityForm with server, client group and
// server IP select options from API lookups (parity with web_vhost_domain_edit).
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const props = defineProps<{
  /** id is the domain_id; absent for the create form. */
  id?: string
}>()

type Opt = { value: string; label: string }
type ServerIPRow = { server_id: number; ip_type: string; ip_address: string }
type ServerRow = { value: string; label: string; server_type?: string }

const overrides = ref<Record<string, Opt[]>>({})
const serverIPs = ref<ServerIPRow[]>([])
/** False when the server-ips lookup failed — keep free-text IP inputs. */
const serverIPsReady = ref(false)
/** server_id → nginx|apache (legacy ajax getservertype / adjustForm). */
const serverTypes = ref<Record<string, string>>({})
/** False when the server lookup failed — avoid defaulting server type. */
const serverTypesReady = ref(false)
const serverTypesError = ref(false)
const ready = ref(false)
const { t } = useI18n()

interface ListResponse {
  items: Record<string, unknown>[]
}

const nginxPHP: Opt[] = [
  { value: 'no', label: 'Disabled' },
  { value: 'php-fpm', label: 'PHP-FPM' },
]
const apachePHP: Opt[] = [
  { value: 'no', label: 'Disabled' },
  { value: 'fast-cgi', label: 'Fast-CGI' },
  { value: 'cgi', label: 'CGI' },
  { value: 'mod', label: 'Mod-PHP' },
  { value: 'suphp', label: 'SuPHP' },
  { value: 'php-fpm', label: 'PHP-FPM' },
]

function serverTypeOf(values: Record<string, unknown>): string | undefined {
  const id = String(values.server_id ?? '')
  if (!serverTypesReady.value || Object.keys(serverTypes.value).length === 0) {
    return undefined
  }
  return serverTypes.value[id]
}

/**
 * ipOptions returns vhost IPs for the selected server and address family.
 * Always keeps the legacy leading entry selectable even when the server has
 * no dedicated IPs: '*' for IPv4, empty "—" for IPv6. The current value is
 * kept in the list so an edit form cannot silently rewrite a removed IP.
 */
function resolveSelectOptions(field: string, values: Record<string, unknown>): Opt[] | undefined {
  if (field === 'php') {
    const st = serverTypeOf(values)
    if (st === undefined) return undefined
    return st === 'apache' ? apachePHP : nginxPHP
  }
  if (field !== 'ip_address' && field !== 'ipv6_address') return undefined
  if (!serverIPsReady.value) return undefined
  const v4 = field === 'ip_address'
  const serverId = String(values.server_id ?? '')
  const rows = serverIPs.value.filter(
    (r) => String(r.server_id) === serverId && r.ip_type === (v4 ? 'IPv4' : 'IPv6'),
  )
  const opts: Opt[] = [
    v4 ? { value: '*', label: '*' } : { value: '', label: '—' },
    ...rows.map((r) => ({ value: r.ip_address, label: r.ip_address })),
  ]
  const current = String(values[field] ?? '')
  if (current && !opts.some((o) => o.value === current)) {
    opts.push({ value: current, label: current })
  }
  return opts
}

/**
 * hideDomainFields mirrors web_vhost_domain_edit.htm adjustForm: Website
 * (vhost) hides type/parent/web_folder; apache-only checkboxes and Options
 * directives follow server_type; PHP Version only for php-fpm/fast-cgi.
 */
function hideDomainFields(values: Record<string, unknown>): string[] {
  const hidden: string[] = []
  const typ = String(values.type ?? 'vhost')
  if (typ === 'vhost' || typ === '') {
    hidden.push('type', 'parent_domain_id', 'web_folder')
  }
  const st = serverTypeOf(values)
  if (st === 'apache') {
    hidden.push('nginx_directives')
  } else if (st !== undefined) {
    hidden.push('perl', 'ruby', 'python', 'suexec', 'apache_directives')
  }
  const php = String(values.php ?? '')
  if (php !== 'php-fpm' && php !== 'fast-cgi' && php !== 'hhvm') {
    hidden.push('server_php_id')
  }
  return hidden
}

onMounted(async () => {
  const o: Record<string, Opt[]> = {}
  try {
    const servers = await api.get<ServerRow[]>('/api/meta/lookups/servers')
    const types: Record<string, string> = {}
    for (const s of servers) {
      types[String(s.value)] = (s.server_type || 'nginx').toLowerCase()
    }
    serverTypes.value = types
    serverTypesReady.value = true
  } catch {
    serverTypesError.value = true
  }
  try {
    serverIPs.value = await api.get<ServerIPRow[]>('/api/meta/lookups/server-ips')
    serverIPsReady.value = true
  } catch {
    // Keep text inputs when the lookup is unavailable.
  }
  try {
    const domains = await api.get<ListResponse>('/api/sites/web-domains?type=vhost&limit=100')
    o.parent_domain_id = [
      { value: '0', label: '—' },
      ...domains.items.map((d) => ({
        value: String(d.domain_id),
        label: String(d.domain),
      })),
    ]
  } catch {
    // Text input fallback for parent_domain_id.
  }
  overrides.value = o
  ready.value = true
})
</script>

<template>
  <UiAlert
    v-if="ready && serverTypesError"
    variant="danger"
    class="mb-3"
    :messages="[t('error.request_failed')]"
  />
  <EntityForm
    v-if="ready"
    entity="web-domains"
    api-base="/api/sites/web-domains"
    back-to="/sites"
    :id="props.id"
    :option-overrides="overrides"
    :resolve-select-options="resolveSelectOptions"
    :metadata-deps="['server_id', 'type', 'php', 'ip_address', 'ipv6_address']"
    :hide-fields="hideDomainFields"
    :readonly-fields="props.id ? ['server_id'] : []"
  />
</template>

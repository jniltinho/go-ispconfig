<script setup lang="ts">
// Web domain form: metadata-driven EntityForm with server, client group and
// server IP select options from API lookups (parity with web_vhost_domain_edit).
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'

const props = defineProps<{
  /** id is the domain_id; absent for the create form. */
  id?: string
}>()

type Opt = { value: string; label: string }
type ServerIPRow = { server_id: number; ip_type: string; ip_address: string }

const overrides = ref<Record<string, Opt[]>>({})
const serverIPs = ref<ServerIPRow[]>([])
const ready = ref(false)

interface ListResponse {
  items: Record<string, unknown>[]
}

/** ipOptions returns vhost IPs for the selected server and address family. */
function ipOptions(field: string, values: Record<string, unknown>): Opt[] | undefined {
  if (field !== 'ip_address' && field !== 'ipv6_address') return undefined
  const ipType = field === 'ip_address' ? 'IPv4' : 'IPv6'
  const serverId = String(values.server_id ?? '')
  const rows = serverIPs.value.filter(
    (r) => String(r.server_id) === serverId && r.ip_type === ipType,
  )
  if (!rows.length) return undefined
  return rows.map((r) => ({ value: r.ip_address, label: r.ip_address }))
}

onMounted(async () => {
  const o: Record<string, Opt[]> = {}
  try {
    const servers = await api.get<Opt[]>('/api/meta/lookups/servers')
    if (servers?.length) {
      o.server_id = servers.map((s) => ({ value: String(s.value), label: String(s.label) }))
    }
  } catch {
    // Fall back to free-text server_id when the lookup is unavailable.
  }
  try {
    const groups = await api.get<Opt[]>('/api/meta/lookups/client-groups')
    if (groups?.length) {
      o.client_group_id = groups.map((g) => ({ value: String(g.value), label: String(g.label) }))
    }
  } catch {
    // client_group_id is admin-only; non-admins never see the field.
  }
  try {
    serverIPs.value = await api.get<ServerIPRow[]>('/api/meta/lookups/server-ips')
  } catch {
    // ip_address / ipv6_address fall back to text inputs.
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
  <EntityForm
    v-if="ready"
    entity="web-domains"
    api-base="/api/sites/web-domains"
    back-to="/sites"
    :id="props.id"
    :option-overrides="overrides"
    :resolve-select-options="ipOptions"
    :readonly-fields="props.id ? ['server_id'] : []"
  />
</template>

<script setup lang="ts">
// Web domain form: metadata-driven EntityForm with server (and parent
// website) select options filled from API lookups so admins never type
// raw ids into server_id / parent_domain_id.
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'

const props = defineProps<{
  /** id is the domain_id; absent for the create form. */
  id?: string
}>()

type Opt = { value: string; label: string }
const overrides = ref<Record<string, Opt[]>>({})
const ready = ref(false)

interface ListResponse {
  items: Record<string, unknown>[]
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
    :readonly-fields="props.id ? ['server_id'] : []"
  />
</template>

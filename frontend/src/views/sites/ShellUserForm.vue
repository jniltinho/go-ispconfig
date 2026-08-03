<script setup lang="ts">
// Shell user form: metadata-driven EntityForm with parent website select.
// On edit, parent_domain_id is locked (API enforces immutability).
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'

const props = defineProps<{
  /** id is the shell_user_id; absent for the create form. */
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
    const domains = await api.get<ListResponse>('/api/sites/web-domains?type=vhost&limit=100')
    o.parent_domain_id = domains.items.map((d) => ({
      value: String(d.domain_id),
      label: String(d.domain),
    }))
  } catch {
    // Text input fallback.
  }
  overrides.value = o
  ready.value = true
})
</script>

<template>
  <EntityForm
    v-if="ready"
    entity="shell-users"
    api-base="/api/sites/shell-users"
    back-to="/sites/shell-users"
    :id="props.id"
    :option-overrides="overrides"
    :readonly-fields="
      props.id
        ? ['server_id', 'username_prefix', 'parent_domain_id']
        : ['server_id', 'username_prefix']
    "
  />
</template>

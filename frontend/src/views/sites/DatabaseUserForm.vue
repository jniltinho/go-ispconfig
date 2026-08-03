<script setup lang="ts">
// Database user form: EntityForm with server select options from the
// shared meta lookup (server_id is otherwise a free-text box).
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'

const props = defineProps<{
  /** id is the database_user_id; absent for create. */
  id?: string
}>()

type Opt = { value: string; label: string }
const overrides = ref<Record<string, Opt[]>>({})
const ready = ref(false)

onMounted(async () => {
  try {
    const servers = await api.get<Opt[]>('/api/meta/lookups/servers')
    if (servers?.length) {
      overrides.value = {
        server_id: [
          { value: '0', label: '—' },
          ...servers.map((s) => ({ value: String(s.value), label: String(s.label) })),
        ],
      }
    }
  } catch {
    // Free-text fallback.
  }
  ready.value = true
})
</script>

<template>
  <EntityForm
    v-if="ready"
    entity="database-users"
    api-base="/api/sites/database-users"
    back-to="/sites/database-users"
    :id="props.id"
    :option-overrides="overrides"
    :readonly-fields="props.id ? ['server_id', 'database_user_prefix'] : []"
  />
</template>

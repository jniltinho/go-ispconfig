<script setup lang="ts">
// Database form: the metadata-driven EntityForm plus API-fed selects so
// the website and database-user references show names instead of raw
// ids (legacy panel datasource parity).
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'
import { useI18n } from '../../i18n'

const props = defineProps<{
  /** id is the database_id; absent for the create form. */
  id?: string
}>()

const { t } = useI18n()

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
    // Free-text server_id when the lookup is unavailable.
  }
  try {
    const domains = await api.get<ListResponse>('/api/sites/web-domains?type=vhost&limit=100')
    o.parent_domain_id = domains.items.map((d) => ({
      value: String(d.domain_id),
      label: String(d.domain),
    }))
  } catch {
    // The field stays a text input when the list cannot be loaded.
  }
  try {
    const users = await api.get<ListResponse>('/api/sites/database-users?limit=100')
    const userOpts = users.items.map((u) => ({
      value: String(u.database_user_id),
      label: String(u.database_user),
    }))
    o.database_user_id = userOpts
    o.database_ro_user_id = [{ value: '', label: t('sites.no_ro_user') }, ...userOpts]
  } catch {
    // Text inputs as fallback.
  }
  overrides.value = o
  ready.value = true
})
</script>

<template>
  <EntityForm
    v-if="ready"
    entity="databases"
    api-base="/api/sites/databases"
    back-to="/sites/databases"
    :id="props.id"
    :option-overrides="overrides"
    :readonly-fields="props.id ? ['server_id', 'database_charset', 'database_name_prefix'] : []"
  />
</template>

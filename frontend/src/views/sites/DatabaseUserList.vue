<script setup lang="ts">
// Database users list: server-side paged DataTable over
// /api/sites/database-users (password hashes never reach the client).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'database_user', label: 'sites.col.database_user' },
  // Decorated via API (_client = "Contact (username)") — legacy PHP
  // Database User list parity; filterable server-side via ListFilters.
  { key: '_client', label: 'sites.col.client' },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const error = ref('')
const loading = ref(false)

async function load(toPage = page.value) {
  error.value = ''
  loading.value = true
  try {
    const res = await store.fetchList('/api/sites/database-users', toPage, pageSize, filters.value)
    rows.value = res.items
    total.value = res.total
    page.value = res.page
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    loading.value = false
  }
}

onMounted(() => load(1))

function onFilter(next: Record<string, string>) {
  filters.value = next
  load(1)
}

function open(row: Row) {
  router.push(`/sites/database-users/${row.database_user_id}`)
}

async function remove(row: Row) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`/api/sites/database-users/${row.database_user_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('sites.database_users_title') }}</h1>
    <button
      type="button"
      class="my-3 btn btn-success px-4 py-2"
      @click="router.push('/sites/database-users/new')"
    >
      {{ t('sites.add_database_user') }}
    </button>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <DataTable
      :columns="columns"
      :rows="rows"
      :total="total"
      :page="page"
      :page-size="pageSize"
      :loading="loading"
      has-actions
      @update:page="load($event)"
      @filter="onFilter"
      @row-click="open"
    >
      <template #actions="{ row }">
        <button
          type="button"
          :title="t('sites.edit')"
          :aria-label="t('sites.edit')"
          class="border border-border bg-surface p-1 hover:bg-info"
          @click="open(row)"
        >
          <component :is="utilityIcons.edit" :size="14" />
        </button>
        <button
          type="button"
          :title="t('sites.delete')"
          :aria-label="t('sites.delete')"
          class="ml-1 border border-danger-border bg-danger p-1 text-danger-text"
          @click.stop="remove(row)"
        >
          <component :is="utilityIcons.delete" :size="14" />
        </button>
      </template>
    </DataTable>
  </div>
</template>

<script setup lang="ts">
// Client databases list: server-side paged DataTable over
// /api/sites/databases with edit, delete and the optional phpMyAdmin
// link action (rendered when the API decorates records with
// _phpmyadmin_url from the configured sites phpmyadmin_url template).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ExternalLink } from 'lucide-vue-next'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

// Display-name columns (_server_name/_parent_domain/_database_user) are
// decorated by the API — legacy panel parity: names, not raw ids. The
// list endpoint resolves them as filter aliases (subqueries on the
// related tables), so their filter boxes search names, not ids.
const columns: Column[] = [
  { key: 'active', label: t('sites.col.active') },
  { key: 'remote_access', label: t('sites.col.remote_access') },
  { key: '_server_name', label: t('sites.col.server') },
  { key: '_parent_domain', label: t('sites.col.website') },
  { key: '_database_user', label: t('sites.col.database_user') },
  { key: 'database_name', label: t('sites.col.database_name') },
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
    const res = await store.fetchList('/api/sites/databases', toPage, pageSize, filters.value)
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
  router.push(`/sites/databases/${row.database_id}`)
}

async function remove(row: Row) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`/api/sites/databases/${row.database_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t('sites.databases_title') }}</h1>
    <button
      type="button"
      class="my-3 btn btn-success px-4 py-2"
      @click="router.push('/sites/databases/new')"
    >
      {{ t('sites.add_database') }}
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
      <template #cell-active="{ value }">
        {{ value === 'y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #cell-remote_access="{ value }">
        {{ value === 'y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #actions="{ row }">
        <a
          v-if="row._phpmyadmin_url"
          :href="String(row._phpmyadmin_url)"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('sites.open_phpmyadmin')"
          :aria-label="t('sites.open_phpmyadmin')"
          data-test="open-phpmyadmin"
          class="inline-block border border-border bg-surface p-1 hover:bg-info"
          @click.stop
        >
          <ExternalLink :size="14" />
        </a>
        <button
          type="button"
          :title="t('sites.edit')"
          :aria-label="t('sites.edit')"
          class="ml-1 border border-border bg-surface p-1 hover:bg-info"
          @click="open(row)"
        >
          <component :is="utilityIcons.edit" :size="14" />
        </button>
        <button
          type="button"
          :title="t('sites.delete')"
          :aria-label="t('sites.delete')"
          class="ml-1 border border-border bg-surface p-1 hover:bg-danger"
          @click.stop="remove(row)"
        >
          <component :is="utilityIcons.delete" :size="14" />
        </button>
      </template>
    </DataTable>
  </div>
</template>

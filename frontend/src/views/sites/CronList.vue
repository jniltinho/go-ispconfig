<script setup lang="ts">
// Cron jobs list: server-side paged DataTable over /api/sites/crons with
// the five schedule fields collapsed into one summary column and the parent
// website / server shown via the API's decorated display-name columns.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'
import { useDialogStore } from '../../stores/dialog'

const { t } = useI18n()
const dialog = useDialogStore()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'active', label: 'sites.col.active' },
  { key: '_parent_domain', label: 'sites.col.website' },
  { key: 'schedule', label: 'sites.col.schedule', filterable: false },
  { key: 'type', label: 'sites.col.type' },
  { key: 'command', label: 'command_txt' },
  { key: 'log', label: 'log_txt' },
  { key: '_server_name', label: 'sites.col.server' },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const error = ref('')
const loading = ref(false)

/** schedule renders the five cron fields as one "min hour mday month wday". */
function schedule(row: Row): string {
  return [row.run_min, row.run_hour, row.run_mday, row.run_month, row.run_wday]
    .map((v) => String(v ?? ''))
    .join(' ')
}

async function load(toPage = page.value) {
  error.value = ''
  loading.value = true
  try {
    const res = await store.fetchList('/api/sites/crons', toPage, pageSize, filters.value)
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
  router.push(`/sites/crons/${row.id}`)
}

async function remove(row: Row) {
  if (!(await dialog.confirm({ message: t('sites.confirm_delete') }))) return
  try {
    await api.delete(`/api/sites/crons/${row.id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('sites.crons_title') }}</h1>
    <button
      type="button"
      class="my-3 btn btn-success px-4 py-2"
      @click="router.push('/sites/crons/new')"
    >
      {{ t('sites.add_cron') }}
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
      <template #cell-log="{ value }">
        {{ value === 'y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #cell-schedule="{ row }">
        <span class="font-mono text-xs">{{ schedule(row) }}</span>
      </template>
      <template #cell-command="{ value }">
        <span class="font-mono text-xs">{{ value }}</span>
      </template>
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
          data-test="delete"
          class="ml-1 border border-danger-border bg-danger p-1 text-danger-text"
          @click="remove(row)"
        >
          <component :is="utilityIcons.delete" :size="14" />
        </button>
      </template>
    </DataTable>
  </div>
</template>

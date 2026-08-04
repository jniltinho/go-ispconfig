<script setup lang="ts">
// ISPConfig log (port of log_list.php): paged sys_log DataTable with the
// admin clear actions. Clearing sets loglevel = 0 server-side — the row is
// never deleted, so the audit trail survives (port of log_del.php).
import { onMounted, ref } from 'vue'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import UiAlert from '../../components/UiAlert.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { useAuthStore } from '../../stores/auth'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import { formatTs } from './state'
import { useDialogStore } from '../../stores/dialog'

const { t } = useI18n()
const dialog = useDialogStore()
const store = useSitesStore()
const auth = useAuthStore()

const columns: Column[] = [
  { key: 'syslog_id', label: 'monitor.col.syslog_id' },
  { key: 'server_id', label: 'monitor.col.server_id' },
  { key: 'loglevel', label: 'monitor.col.loglevel' },
  { key: 'tstamp', label: 'monitor.col.tstamp', filterable: false },
  { key: 'message', label: 'monitor.col.message' },
]

const levels = ['monitor.level.0', 'monitor.level.1', 'monitor.level.2']

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const error = ref('')
const loading = ref(false)
const batchLevel = ref('2')

async function load(p?: number) {
  error.value = ''
  loading.value = true
  try {
    const res: ListResponse = await store.fetchList(
      '/api/monitor/sys-log',
      p ?? page.value,
      pageSize,
      filters.value,
    )
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

function onFilter(f: Record<string, string>) {
  filters.value = f
  load(1)
}

async function clearOne(row: Row) {
  await clear({ syslog_id: Number(row.syslog_id) })
}

async function clearLevel() {
  if (!(await dialog.confirm({ message: t('monitor.confirm_clear_level') }))) return
  await clear({ loglevel: Number(batchLevel.value) })
}

async function clear(body: Record<string, number>) {
  error.value = ''
  try {
    await api.post('/api/monitor/sys-log/clear', body)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('monitor.syslog_title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="my-3" :messages="[t(error)]" />

    <div v-if="auth.typ === 'admin'" class="my-3 flex items-center gap-2">
      <label class="text-sm" for="monitor-batch-level">{{ t('monitor.clear_level') }}</label>
      <select
        id="monitor-batch-level"
        v-model="batchLevel"
        data-test="monitor-batch-level"
        class="border border-border bg-surface px-2 py-1 text-sm text-text"
      >
        <option v-for="(key, level) in levels" :key="level" :value="String(level)">
          {{ t(key) }}
        </option>
      </select>
      <button
        type="button"
        data-test="monitor-clear-level"
        class="btn btn-danger px-4 py-2"
        @click="clearLevel"
      >
        {{ t('monitor.clear') }}
      </button>
    </div>

    <DataTable
      :columns="columns"
      :rows="rows"
      :total="total"
      :page="page"
      :page-size="pageSize"
      :loading="loading"
      :has-actions="auth.typ === 'admin'"
      @update:page="load($event)"
      @filter="onFilter"
    >
      <template #cell-tstamp="{ value }">{{ formatTs(Number(value)) }}</template>
      <template #cell-loglevel="{ value }">{{ t(`monitor.level.${value}`) }}</template>
      <template #actions="{ row }">
        <button
          type="button"
          data-test="monitor-clear-one"
          class="btn btn-default px-2 py-1 text-xs"
          @click="clearOne(row)"
        >
          {{ t('monitor.clear') }}
        </button>
      </template>
    </DataTable>
  </div>
</template>

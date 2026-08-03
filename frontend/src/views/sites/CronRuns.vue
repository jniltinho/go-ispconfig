<script setup lang="ts">
// Run history of one cron job: paginated sys_log rows from
// /api/sites/crons/:id/runs (task 4.4), newest first.
import { onMounted, ref } from 'vue'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const props = defineProps<{
  /** id is the cron id whose runs are listed. */
  id: string
}>()

const { t } = useI18n()
const store = useSitesStore()

// Filters are not supported by the runs endpoint: header inputs stay off.
const columns: Column[] = [
  { key: 'start', label: 'sites.cron.col.start', filterable: false },
  { key: 'status', label: 'sites.cron.col.status', filterable: false },
  { key: 'exit', label: 'sites.cron.col.exit', filterable: false },
  { key: 'duration', label: 'sites.cron.col.duration', filterable: false },
  { key: 'output', label: 'sites.cron.col.output', filterable: false },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const error = ref('')
const loading = ref(false)

/** stamp formats a unix time; run rows without one fall back to sys_log tstamp. */
function stamp(row: Row): string {
  const unix = Number(row.start) || Number(row.tstamp) || 0
  return unix ? new Date(unix * 1000).toLocaleString() : '-'
}

/** duration is end-start in seconds, '-' when the run never finished. */
function duration(row: Row): string {
  const seconds = Number(row.end) - Number(row.start)
  return Number.isFinite(seconds) && seconds >= 0 ? `${seconds}s` : '-'
}

async function load(toPage = page.value) {
  error.value = ''
  loading.value = true
  try {
    const res = await store.fetchList(`/api/sites/crons/${props.id}/runs`, toPage, pageSize)
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
</script>

<template>
  <section data-test="cron-runs">
    <h2 class="mb-2 text-base font-bold">{{ t('sites.cron.runs_title') }}</h2>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <DataTable
      :columns="columns"
      :rows="rows"
      :total="total"
      :page="page"
      :page-size="pageSize"
      :loading="loading"
      @update:page="load($event)"
    >
      <template #cell-start="{ row }">{{ stamp(row) }}</template>
      <template #cell-duration="{ row }">{{ duration(row) }}</template>
      <template #cell-output="{ value }">
        <span class="font-mono text-xs whitespace-pre-wrap">{{ value }}</span>
      </template>
    </DataTable>
  </section>
</template>

<script setup lang="ts">
// Read-only sys_datalog DataTable, shared by Jobqueue (pending rows) and
// Data log history (everything). Both endpoints return the same DatalogList
// body, so a single view with a different apiBase covers them.
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import UiAlert from '../../components/UiAlert.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { ApiError } from '../../api'
import { useI18n } from '../../i18n'
import { formatTs } from './state'

const props = defineProps<{
  apiBase: string
  titleKey: string
  /** detailBase enables row-click navigation to the entry detail view. */
  detailBase?: string
}>()

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'datalog_id', label: 'monitor.col.datalog_id' },
  { key: 'server_id', label: 'monitor.col.server_id' },
  { key: 'dbtable', label: 'monitor.col.dbtable' },
  { key: 'dbidx', label: 'monitor.col.dbidx' },
  { key: 'action', label: 'monitor.col.action' },
  { key: 'user', label: 'monitor.col.user' },
  { key: 'tstamp', label: 'monitor.col.tstamp', filterable: false },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const error = ref('')
const loading = ref(false)

async function load(p?: number) {
  error.value = ''
  loading.value = true
  try {
    const res: ListResponse = await store.fetchList(
      props.apiBase,
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
// Jobqueue and history share this component, so a switch between the two
// routes must refetch instead of reusing the previous list.
watch(() => props.apiBase, () => load(1))

function onFilter(f: Record<string, string>) {
  filters.value = f
  load(1)
}

function open(row: Row) {
  if (props.detailBase) router.push(`${props.detailBase}/${row.datalog_id}`)
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t(titleKey) }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <DataTable
      :columns="columns"
      :rows="rows"
      :total="total"
      :page="page"
      :page-size="pageSize"
      :loading="loading"
      @update:page="load($event)"
      @filter="onFilter"
      @row-click="open"
    >
      <template #cell-tstamp="{ value }">{{ formatTs(Number(value)) }}</template>
      <template #cell-action="{ value }">{{ t(`monitor.action.${value}`) }}</template>
    </DataTable>
  </div>
</template>

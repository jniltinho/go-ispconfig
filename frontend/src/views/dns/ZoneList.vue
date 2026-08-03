<script setup lang="ts">
// DNS zone list: server-side paged/filtered dns_soa DataTable with datalog
// pending/error badges, wizard + manual-create entry points, edit/delete.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'active', label: t('sites.col.active') },
  { key: '_server_name', label: t('sites.col.server') },
  { key: 'origin', label: t('dns.col.zone') },
  { key: 'ns', label: t('dns.col.ns') },
  { key: 'mbox', label: t('dns.col.mbox') },
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
    const res: ListResponse = await store.fetchList('/api/dns/zones', p ?? page.value, pageSize, filters.value)
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

function open(row: Row) {
  router.push(`/dns/zones/${row.id}`)
}

async function remove(row: Row) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`/api/dns/zones/${row.id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t('dns.zones_title') }}</h1>
    <div class="my-3 flex gap-2">
      <button
        type="button"
        data-test="add-zone-wizard"
        class="btn btn-success px-4 py-2"
        @click="router.push('/dns/wizard')"
      >
        {{ t('dns.add_zone_wizard') }}
      </button>
      <button
        type="button"
        data-test="add-zone-manual"
        class="btn btn-default px-4 py-2"
        @click="router.push('/dns/zones/new')"
      >
        {{ t('dns.add_zone_manual') }}
      </button>
    </div>

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
        {{ value === 'Y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #cell-origin="{ row, value }">
        <span>{{ value }}</span>
        <span
          v-if="row._datalog_state === 'pending'"
          class="ml-2 bg-info px-1.5 py-0.5 text-xs font-bold text-link"
          data-test="state-pending"
        >
          {{ t('sites.state.pending') }}
        </span>
        <span
          v-else-if="row._datalog_state === 'error'"
          class="ml-2 bg-danger px-1.5 py-0.5 text-xs font-bold text-danger-text"
          data-test="state-error"
          :title="String(row._datalog_error ?? '')"
        >
          {{ t('sites.state.error') }}
        </span>
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

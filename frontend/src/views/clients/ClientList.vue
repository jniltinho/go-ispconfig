<script setup lang="ts">
// Clients list (limit_client = 0 rows): server-side paged/filtered
// DataTable; delete opens the owned-resource confirmation dialog.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const props = withDefaults(
  defineProps<{
    /** apiBase distinguishes the clients and resellers surfaces. */
    apiBase?: string
    /** formBase is the route prefix of the edit form. */
    formBase?: string
    titleKey?: string
    addKey?: string
  }>(),
  {
    apiBase: '/api/clients',
    formBase: '/clients',
    titleKey: 'client.clients_title',
    addKey: 'client.add_client',
  },
)

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'company_name', label: t('company_name_txt') },
  { key: 'contact_name', label: t('contact_name_txt') },
  { key: 'username', label: t('username_txt') },
  { key: 'customer_no', label: t('customer_no_txt') },
  { key: 'locked', label: t('locked_txt') },
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
    const res: ListResponse = await store.fetchList(props.apiBase, p ?? page.value, pageSize, filters.value)
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
  router.push(`${props.formBase}/${row.client_id}`)
}

// ponytail: plain confirm; the owned-resource count dialog lands with
// task 5.5.
async function remove(row: Row) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`${props.apiBase}/${row.client_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t(titleKey) }}</h1>
    <button
      type="button"
      data-test="add-client"
      class="my-3 btn btn-success px-4 py-2"
      @click="router.push(`${formBase}/new`)"
    >
      {{ t(addKey) }}
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
      <template #cell-locked="{ row }">
        <span v-if="row.locked === 'y'" class="bg-danger px-1.5 py-0.5 text-xs font-bold text-danger-text">
          {{ t('locked_txt') }}
        </span>
        <span v-else-if="row.canceled === 'y'" class="bg-info px-1.5 py-0.5 text-xs font-bold">
          {{ t('canceled_txt') }}
        </span>
        <span v-else>{{ t('client.status_active') }}</span>
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

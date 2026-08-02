<script setup lang="ts">
// Admin zone template management list (dns_template CRUD).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Pencil, Trash2 } from 'lucide-vue-next'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'visible', label: t('dns.col.visible') },
  { key: 'name', label: t('dns.col.template_name') },
  { key: 'fields', label: t('dns.col.template_fields') },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const error = ref('')

async function load(p?: number) {
  error.value = ''
  try {
    const res: ListResponse = await store.fetchList('/api/dns/zone-templates', p ?? page.value, pageSize, filters.value)
    rows.value = res.items
    total.value = res.total
    page.value = res.page
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

onMounted(() => load(1))

function onFilter(f: Record<string, string>) {
  filters.value = f
  load(1)
}

function open(row: Row) {
  router.push(`/dns/templates/${row.template_id}`)
}

async function remove(row: Row) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`/api/dns/zone-templates/${row.template_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t('dns.templates_title') }}</h1>
    <button
      type="button"
      data-test="add-template"
      class="my-3 bg-success px-4 py-2 text-xs font-bold text-white hover:opacity-90"
      @click="router.push('/dns/templates/new')"
    >
      {{ t('dns.add_template') }}
    </button>

    <p
      v-if="error"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
    >
      {{ t(error) }}
    </p>

    <DataTable
      :columns="columns"
      :rows="rows"
      :total="total"
      :page="page"
      :page-size="pageSize"
      has-actions
      @update:page="load($event)"
      @filter="onFilter"
      @row-click="open"
    >
      <template #cell-visible="{ value }">
        {{ String(value).toLowerCase() === 'y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #actions="{ row }">
        <button
          type="button"
          :title="t('sites.edit')"
          class="border border-border bg-surface p-1 hover:bg-info"
          @click="open(row)"
        >
          <Pencil :size="14" />
        </button>
        <button
          type="button"
          :title="t('sites.delete')"
          data-test="delete"
          class="ml-1 border border-danger-border bg-danger p-1 text-danger-text"
          @click="remove(row)"
        >
          <Trash2 :size="14" />
        </button>
      </template>
    </DataTable>
  </div>
</template>

<script setup lang="ts">
// Protected Folders list: server-side paged DataTable over
// /api/sites/web-folders with edit and per-folder Users navigation.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Pencil, Users } from 'lucide-vue-next'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { ApiError } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'active', label: t('sites.col.active') },
  { key: 'server_id', label: t('sites.col.server') },
  { key: 'parent_domain_id', label: t('sites.col.website') },
  { key: 'path', label: t('sites.col.path') },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const error = ref('')

async function load(toPage = page.value) {
  error.value = ''
  try {
    const res = await store.fetchList('/api/sites/web-folders', toPage, pageSize, filters.value)
    rows.value = res.items
    total.value = res.total
    page.value = res.page
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

onMounted(() => load(1))

function onFilter(next: Record<string, string>) {
  filters.value = next
  load(1)
}

function open(row: Row) {
  router.push(`/sites/folders/${row.web_folder_id}`)
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t('sites.folders_title') }}</h1>
    <button
      type="button"
      class="my-3 bg-success px-4 py-2 text-xs font-bold text-white hover:opacity-90"
      @click="router.push('/sites/folders/new')"
    >
      {{ t('sites.add_folder') }}
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
      <template #cell-active="{ value }">
        {{ value === 'y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #actions="{ row }">
        <button
          type="button"
          :title="t('sites.users')"
          data-test="folder-users"
          class="border border-border bg-surface p-1 hover:bg-info"
          @click="router.push(`/sites/folders/${row.web_folder_id}/users`)"
        >
          <Users :size="14" />
        </button>
        <button
          type="button"
          :title="t('sites.edit')"
          class="ml-1 border border-border bg-surface p-1 hover:bg-info"
          @click="open(row)"
        >
          <Pencil :size="14" />
        </button>
      </template>
    </DataTable>
  </div>
</template>

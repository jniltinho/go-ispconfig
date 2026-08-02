<script setup lang="ts">
// Folder Users list: the web_folder_user records of one protected folder
// (filtered by web_folder_id on the server side).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const props = defineProps<{ folderId: string }>()

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'active', label: t('sites.col.active') },
  { key: 'username', label: t('sites.col.username') },
]

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const error = ref('')

async function load(toPage = page.value) {
  error.value = ''
  try {
    const res = await store.fetchList('/api/sites/web-folder-users', toPage, pageSize, {
      web_folder_id: props.folderId,
    })
    rows.value = res.items
    total.value = res.total
    page.value = res.page
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

onMounted(() => load(1))

function open(row: Row) {
  router.push(`/sites/folders/${props.folderId}/users/${row.web_folder_user_id}`)
}

async function remove(row: Row) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`/api/sites/web-folder-users/${row.web_folder_user_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t('sites.folder_users_title') }}</h1>
    <button
      type="button"
      class="my-3 bg-success px-4 py-2 text-xs font-bold text-white hover:opacity-90"
      @click="router.push(`/sites/folders/${props.folderId}/users/new`)"
    >
      {{ t('sites.add_folder_user') }}
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
      @row-click="open"
    >
      <template #cell-active="{ value }">
        {{ value === 'y' ? t('yes_txt') : t('no_txt') }}
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

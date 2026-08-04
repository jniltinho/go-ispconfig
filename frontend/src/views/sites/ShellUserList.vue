<script setup lang="ts">
// Shell users list: server-side paged DataTable over /api/sites/shell-users
// (password hashes never reach the client).
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
  { key: 'username', label: 'sites.col.username' },
  { key: '_parent_domain', label: 'sites.col.website' },
  { key: 'chroot', label: 'sites.col.chroot' },
  { key: 'active', label: 'sites.col.active' },
  { key: '_server_name', label: 'sites.col.server' },
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
    const res = await store.fetchList('/api/sites/shell-users', toPage, pageSize, filters.value)
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
  router.push(`/sites/shell-users/${row.shell_user_id}`)
}

async function remove(row: Row) {
  if (!(await dialog.confirm({ message: t('sites.confirm_delete') }))) return
  try {
    await api.delete(`/api/sites/shell-users/${row.shell_user_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('sites.shell_users_title') }}</h1>
    <button
      type="button"
      class="my-3 btn btn-success px-4 py-2"
      data-test="add-shell-user"
      @click="router.push('/sites/shell-users/new')"
    >
      {{ t('sites.add_shell_user') }}
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
      <template #cell-chroot="{ value }">
        {{ value === 'jailkit' ? t('jailkit_txt') : t('no_txt') }}
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
          class="ml-1 border border-danger-border bg-danger p-1 text-danger-text"
          @click.stop="remove(row)"
        >
          <component :is="utilityIcons.delete" :size="14" />
        </button>
      </template>
    </DataTable>
  </div>
</template>

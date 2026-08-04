<script setup lang="ts">
// Client email message templates (welcome/gdpr/other), ownership-scoped.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'
import { useDialogStore } from '../../stores/dialog'

const { t } = useI18n()
const dialog = useDialogStore()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'template_name', label: 'template_name_txt' },
  { key: 'template_type', label: 'template_type_txt' },
  { key: 'subject', label: 'subject_txt' },
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
    const res: ListResponse = await store.fetchList('/api/client-message-templates', p ?? page.value, pageSize, filters.value)
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
  router.push(`/clients/message-templates/${row.client_message_template_id}`)
}

async function remove(row: Row) {
  if (!(await dialog.confirm({ message: t('sites.confirm_delete') }))) return
  try {
    await api.delete(`/api/client-message-templates/${row.client_message_template_id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('client.message_templates_title') }}</h1>
    <p class="mt-1 text-sm text-text/70">{{ t('client.placeholder_help') }}</p>
    <button
      type="button"
      data-test="add-message-template"
      class="my-3 btn btn-success px-4 py-2"
      @click="router.push('/clients/message-templates/new')"
    >
      {{ t('client.add_message_template') }}
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

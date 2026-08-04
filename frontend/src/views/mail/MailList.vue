<script setup lang="ts">
// Generic mail DataTable list: paged/filtered rows from an API endpoint
// with edit/delete and datalog badges. Every mail list is a thin wrapper
// passing apiBase/idField/columns/formBase — no per-list boilerplate.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { utilityIcons } from '../../icons'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore, type ListResponse } from '../../stores/sites'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'
import { useDialogStore } from '../../stores/dialog'

const props = defineProps<{
  apiBase: string
  idField: string
  formBase: string
  columns: Column[]
  titleKey: string
  addKey: string
  /** activeValue is the "on" value for the status badge ('y' or 'Y'). */
  activeValue?: string
  /**
   * noDelete drops the per-row delete button, for lists that only pick a
   * record to edit (Server Config picks the node whose INI is edited;
   * deleting the server from there would be a different, destructive action).
   *
   * Phrased as an opt-out, not an opt-in: Vue casts an absent Boolean-typed
   * prop to `false`, so a `deletable?: boolean` default would silently hide
   * the button on every list that does not pass it.
   */
  noDelete?: boolean
}>()

const { t } = useI18n()
const dialog = useDialogStore()
const router = useRouter()
const store = useSitesStore()

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 25
const filters = ref<Record<string, string>>({})
const order = ref('')
const error = ref('')
const loading = ref(false)

async function load(p?: number) {
  error.value = ''
  loading.value = true
  try {
    const res: ListResponse = await store.fetchList(props.apiBase, p ?? page.value, pageSize, {
      ...filters.value,
      ...(order.value ? { order: order.value } : {}),
    })
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

function onSort(o: string) {
  order.value = o
  load(1)
}

function open(row: Row) {
  router.push(`${props.formBase}/${row[props.idField]}`)
}

async function remove(row: Row) {
  if (!(await dialog.confirm({ message: t('sites.confirm_delete') }))) return
  try {
    await api.delete(`${props.apiBase}/${row[props.idField]}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}

const activeOn = props.activeValue ?? 'y'
</script>

<template>
  <div>
    <h1 class="page-title">{{ t(titleKey) }}</h1>
    <button
      v-if="addKey"
      type="button"
      data-test="mail-add"
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
      sortable
      @update:page="load($event)"
      @filter="onFilter"
      @row-click="open"
      @sort="onSort"
    >
      <template #cell-active="{ value }">
        {{ String(value) === activeOn ? t('yes_txt') : t('no_txt') }}
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
          v-if="!noDelete"
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

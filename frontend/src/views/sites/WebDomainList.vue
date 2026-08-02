<script setup lang="ts">
// Websites list: server-side paged/filtered domain DataTable with datalog
// pending/error indicators; row click (or the edit action) opens the form.
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Pencil } from 'lucide-vue-next'
import DataTable, { type Column, type Row } from '../../components/DataTable.vue'
import { useSitesStore } from '../../stores/sites'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()
const store = useSitesStore()

const columns: Column[] = [
  { key: 'active', label: t('sites.col.active') },
  { key: 'server_id', label: t('sites.col.server') },
  { key: 'domain', label: t('sites.col.domain') },
  { key: 'type', label: t('sites.col.type') },
]

onMounted(() => store.loadDomains(1))

function onFilter(filters: Record<string, string>) {
  store.filters = filters
  store.loadDomains(1)
}

function open(row: Row) {
  router.push(`/sites/domains/${row.domain_id}`)
}
</script>

<template>
  <div>
    <h1 class="text-lg font-bold">{{ t('sites.websites_title') }}</h1>
    <button
      type="button"
      class="my-3 bg-success px-4 py-2 text-xs font-bold text-white hover:opacity-90"
      @click="router.push('/sites/domains/new')"
    >
      {{ t('sites.add_website') }}
    </button>

    <p
      v-if="store.error"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
    >
      {{ t(store.error) }}
    </p>

    <DataTable
      :columns="columns"
      :rows="store.domains"
      :total="store.total"
      :page="store.page"
      :page-size="store.pageSize"
      has-actions
      @update:page="store.loadDomains($event)"
      @filter="onFilter"
      @row-click="open"
    >
      <template #cell-active="{ value }">
        {{ value === 'y' ? t('yes_txt') : t('no_txt') }}
      </template>
      <template #cell-domain="{ row, value }">
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
      <template #cell-type="{ value }">
        {{ t(`type_${value}_txt`) }}
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
      </template>
    </DataTable>
  </div>
</template>

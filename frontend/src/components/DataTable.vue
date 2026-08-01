<script setup lang="ts">
// ISPConfig-style list view: dark thead with a second row of inline column
// filters, zebra striping, hover highlight, right-aligned actions column and
// server-side pagination driven by props/events.
import { computed, reactive } from 'vue'
import { Filter } from 'lucide-vue-next'
import { useI18n } from '../i18n'

export interface Column {
  key: string
  label: string
  filterable?: boolean
}

export type Row = Record<string, unknown>

const props = defineProps<{
  columns: Column[]
  rows: Row[]
  total: number
  page: number
  pageSize: number
  hasActions?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:page', page: number): void
  (e: 'filter', filters: Record<string, string>): void
}>()

const { t } = useI18n()
const filters = reactive<Record<string, string>>({})

const pages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))
const colCount = computed(() => props.columns.length + (props.hasActions ? 1 : 0))

function applyFilters() {
  const active: Record<string, string> = {}
  for (const [key, value] of Object.entries(filters)) {
    if (value !== '') active[key] = value
  }
  emit('filter', active)
}

function goTo(page: number) {
  if (page >= 1 && page <= pages.value && page !== props.page) emit('update:page', page)
}
</script>

<template>
  <div class="overflow-x-auto border border-border bg-surface">
    <table class="w-full border-collapse text-sm">
      <thead class="bg-thead text-white">
        <tr>
          <th
            v-for="col in columns"
            :key="col.key"
            class="px-3 py-2.5 text-left text-xs font-bold uppercase"
          >
            {{ col.label }}
          </th>
          <th v-if="hasActions" class="px-3 py-2.5 text-right text-xs font-bold uppercase">
            {{ t('table.actions') }}
          </th>
        </tr>
        <!-- Inline filter row (signature ISPConfig trait) -->
        <tr>
          <th v-for="col in columns" :key="col.key" class="px-2 py-1.5">
            <input
              v-if="col.filterable !== false"
              v-model="filters[col.key]"
              type="text"
              class="w-full border border-border bg-surface px-2 py-1 text-xs font-normal text-text outline-none"
              @keyup.enter="applyFilters"
            />
          </th>
          <th v-if="hasActions" class="px-2 py-1.5 text-right">
            <button
              type="button"
              :title="t('table.filter')"
              class="border border-border bg-surface p-1 text-text hover:bg-info"
              @click="applyFilters"
            >
              <Filter :size="14" />
            </button>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="rows.length === 0">
          <td :colspan="colCount" class="px-3 py-6 text-center text-text">
            {{ t('table.empty') }}
          </td>
        </tr>
        <tr
          v-for="(row, i) in rows"
          :key="i"
          class="border-t border-border odd:bg-bg hover:bg-info"
        >
          <td v-for="col in columns" :key="col.key" class="px-3 py-2">
            <slot :name="`cell-${col.key}`" :row="row" :value="row[col.key]">
              {{ row[col.key] }}
            </slot>
          </td>
          <td v-if="hasActions" class="px-3 py-2 text-right whitespace-nowrap">
            <slot name="actions" :row="row" />
          </td>
        </tr>
      </tbody>
      <tfoot>
        <tr class="border-t border-border bg-bg">
          <td :colspan="colCount" class="px-3 py-2">
            <div class="flex items-center justify-between text-xs">
              <span>{{ t('table.total_records', { total }) }}</span>
              <div class="flex items-center gap-2">
                <button
                  type="button"
                  class="border border-border bg-surface px-2 py-1 disabled:opacity-50"
                  :disabled="page <= 1"
                  @click="goTo(page - 1)"
                >
                  {{ t('table.prev') }}
                </button>
                <span>{{ t('table.page_of', { page, pages }) }}</span>
                <button
                  type="button"
                  class="border border-border bg-surface px-2 py-1 disabled:opacity-50"
                  :disabled="page >= pages"
                  @click="goTo(page + 1)"
                >
                  {{ t('table.next') }}
                </button>
              </div>
            </div>
          </td>
        </tr>
      </tfoot>
    </table>
  </div>
</template>

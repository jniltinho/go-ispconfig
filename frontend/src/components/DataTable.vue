<script setup lang="ts">
// ISPConfig-style list view: dark thead with a second row of inline column
// filters, zebra striping, hover highlight, right-aligned actions column and
// server-side pagination driven by props/events.
import { computed, reactive } from 'vue'
import { utilityIcons } from '../icons'
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
  /** loading renders skeleton rows instead of data (D7). */
  loading?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:page', page: number): void
  (e: 'filter', filters: Record<string, string>): void
  (e: 'row-click', row: Row): void
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
        <!-- Inline filter row (signature ISPConfig trait). The filter
             button also renders on tables without an actions column. -->
        <tr>
          <th v-for="(col, idx) in columns" :key="col.key" class="px-2 py-1.5">
            <div class="flex items-center gap-1">
              <input
                v-if="col.filterable !== false"
                v-model="filters[col.key]"
                type="text"
                :aria-label="`${t('table.filter')}: ${col.label}`"
                class="w-full border border-border bg-surface px-2 py-1 text-xs font-normal text-text outline-none focus:border-link"
                @keyup.enter="applyFilters"
              />
              <button
                v-if="!hasActions && idx === columns.length - 1"
                type="button"
                :title="t('table.filter')"
                :aria-label="t('table.filter')"
                class="border border-border bg-surface p-1 text-text transition-colors duration-150 hover:bg-info"
                @click="applyFilters"
              >
                <component :is="utilityIcons.filter" :size="14" />
              </button>
            </div>
          </th>
          <th v-if="hasActions" class="px-2 py-1.5 text-right">
            <button
              type="button"
              :title="t('table.filter')"
              :aria-label="t('table.filter')"
              class="border border-border bg-surface p-1 text-text transition-colors duration-150 hover:bg-info"
              @click="applyFilters"
            >
              <component :is="utilityIcons.filter" :size="14" />
            </button>
          </th>
        </tr>
      </thead>
      <tbody>
        <!-- Loading: skeleton rows sized like real ones -->
        <template v-if="loading">
          <tr v-for="n in 5" :key="`skeleton-${n}`" class="border-t border-border" data-test="skeleton-row">
            <td v-for="col in columns" :key="col.key" class="px-3 py-2">
              <div class="h-4 animate-pulse bg-border/60" />
            </td>
            <td v-if="hasActions" class="px-3 py-2">
              <div class="ml-auto h-4 w-12 animate-pulse bg-border/60" />
            </td>
          </tr>
        </template>
        <!-- Zero results: icon + hint instead of a bare empty body -->
        <tr v-else-if="rows.length === 0" data-test="empty-state">
          <td :colspan="colCount" class="px-3 py-10 text-center">
            <component :is="utilityIcons.search" :size="28" class="mx-auto mb-2 text-text-muted" />
            <p class="text-sm font-semibold">{{ t('table.empty') }}</p>
            <p class="mt-1 text-xs text-text-muted">{{ t('table.empty_hint') }}</p>
          </td>
        </tr>
        <tr
          v-for="(row, i) in loading ? [] : rows"
          :key="i"
          class="cursor-pointer border-t border-border odd:bg-bg hover:bg-info"
          @click="emit('row-click', row)"
        >
          <td v-for="col in columns" :key="col.key" class="px-3 py-2">
            <slot :name="`cell-${col.key}`" :row="row" :value="row[col.key]">
              {{ row[col.key] }}
            </slot>
          </td>
          <td v-if="hasActions" class="px-3 py-2 text-right whitespace-nowrap" @click.stop>
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
                  class="btn btn-default px-2 py-1"
                  :disabled="page <= 1"
                  @click="goTo(page - 1)"
                >
                  {{ t('table.prev') }}
                </button>
                <span>{{ t('table.page_of', { page, pages }) }}</span>
                <button
                  type="button"
                  class="btn btn-default px-2 py-1"
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

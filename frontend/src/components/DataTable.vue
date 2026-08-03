<script setup lang="ts">
// ISPConfig-style list view: dark thead with a second row of inline column
// filters, zebra striping, hover highlight, right-aligned actions column and
// server-side pagination driven by props/events.
import { computed, reactive, ref } from 'vue'
import { utilityIcons } from '../icons'
import { useI18n } from '../i18n'

export interface Column {
  key: string
  label: string
  filterable?: boolean
  /** sortable=false opts a column out of header sorting. */
  sortable?: boolean
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
  /** sortable enables header-click sorting (emits 'sort' with field[.desc]). */
  sortable?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:page', page: number): void
  (e: 'filter', filters: Record<string, string>): void
  (e: 'row-click', row: Row): void
  (e: 'sort', order: string): void
}>()

// Header sort state: asc → desc → off, server-side via the 'sort' event.
const sort = ref<{ key: string; desc: boolean } | null>(null)
function toggleSort(col: Column) {
  if (!props.sortable || col.sortable === false || col.key.startsWith('_')) return
  sort.value =
    sort.value?.key !== col.key
      ? { key: col.key, desc: false }
      : sort.value.desc
        ? null
        : { key: col.key, desc: true }
  emit('sort', sort.value ? `${sort.value.key}${sort.value.desc ? '.desc' : ''}` : '')
}

const { t } = useI18n()
const filters = reactive<Record<string, string>>({})

const pages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))
// A trailing cell always exists: the actions column or the filter-button
// column on action-less tables.
const colCount = computed(() => props.columns.length + 1)

// hasActiveFilters steers the empty-state hint text.
const hasActiveFilters = computed(() => Object.values(filters).some((v) => v !== ''))

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
    <table class="w-full border-collapse text-sm" :aria-busy="loading || undefined">
      <thead class="bg-thead text-white">
        <tr>
          <th
            v-for="col in columns"
            :key="col.key"
            class="px-3 py-2.5 text-left text-xs font-bold uppercase"
            :aria-sort="
              sort?.key === col.key ? (sort.desc ? 'descending' : 'ascending') : undefined
            "
          >
            <!-- t() passes unknown keys through, so labels may be plain
                 strings or i18n keys (router-driven lists use keys).
                 Decorated columns (_prefix) are not DB fields → not sortable. -->
            <button
              v-if="sortable && col.sortable !== false && !col.key.startsWith('_')"
              type="button"
              class="cursor-pointer text-xs font-bold uppercase text-white"
              @click="toggleSort(col)"
            >
              {{ t(col.label) }}
              <span aria-hidden="true">{{
                sort?.key === col.key ? (sort.desc ? '▼' : '▲') : ''
              }}</span>
            </button>
            <template v-else>{{ t(col.label) }}</template>
          </th>
          <th class="px-3 py-2.5 text-right text-xs font-bold uppercase">
            <template v-if="hasActions">{{ t('table.actions') }}</template>
          </th>
        </tr>
        <!-- Inline filter row (signature ISPConfig trait); the trailing
             cell always hosts the filter action button. Legacy puts it on a
             light band below the dark head, not on the dark head itself. -->
        <tr class="bg-bg text-text">
          <th v-for="col in columns" :key="col.key" class="px-2 py-1.5">
            <input
              v-if="col.filterable !== false"
              v-model="filters[col.key]"
              type="text"
              :aria-label="`${t('table.filter')}: ${t(col.label)}`"
              class="w-full border border-border bg-surface px-2 py-1 text-xs font-normal text-text outline-none focus:border-link"
              @keyup.enter="applyFilters"
            />
          </th>
          <th class="px-2 py-1.5 text-right">
            <button
              type="button"
              :title="t('table.filter')"
              :aria-label="t('table.filter')"
              class="btn btn-default p-1"
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
          <tr v-for="n in Math.min(pageSize, 10)" :key="`skeleton-${n}`" class="border-t border-border" data-test="skeleton-row">
            <td v-for="col in columns" :key="col.key" class="px-3 py-2">
              <div class="h-4 motion-safe:animate-pulse bg-border/60" />
            </td>
            <td class="px-3 py-2">
              <div v-if="hasActions" class="ml-auto h-4 w-12 motion-safe:animate-pulse bg-border/60" />
            </td>
          </tr>
        </template>
        <!-- Zero results: icon + hint instead of a bare empty body -->
        <tr v-else-if="rows.length === 0" data-test="empty-state">
          <td :colspan="colCount" class="px-3 py-10 text-center">
            <component :is="utilityIcons.search" :size="28" class="mx-auto mb-2 text-text-muted" />
            <p class="text-sm font-semibold">{{ t('table.empty') }}</p>
            <p class="mt-1 text-xs text-text-muted">
              {{ t(hasActiveFilters ? 'table.empty_hint_filtered' : 'table.empty_hint') }}
            </p>
          </td>
        </tr>
        <template v-else>
        <tr
          v-for="(row, i) in rows"
          :key="i"
          class="cursor-pointer border-t border-border odd:bg-bg hover:bg-info"
          @click="emit('row-click', row)"
        >
          <td v-for="col in columns" :key="col.key" class="px-3 py-2">
            <slot :name="`cell-${col.key}`" :row="row" :value="row[col.key]">
              {{ row[col.key] }}
            </slot>
          </td>
          <td class="px-3 py-2 text-right whitespace-nowrap" @click.stop>
            <slot v-if="hasActions" name="actions" :row="row" />
          </td>
        </tr>
        </template>
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
                  :disabled="loading || page <= 1"
                  @click="goTo(page - 1)"
                >
                  {{ t('table.prev') }}
                </button>
                <span>{{ t('table.page_of', { page, pages }) }}</span>
                <button
                  type="button"
                  class="btn btn-default px-2 py-1"
                  :disabled="loading || page >= pages"
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

<script setup lang="ts">
// Check detail (port of show_data.php / show_log.php): the newest sample of
// one monitor_data type per server. Three payload shapes cover every type —
// a log tail ({output: string}), a table of rows (disk_usage: numbered
// objects) and a flat key/value map (everything else).
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import UiAlert from '../../components/UiAlert.vue'
import { formatTs, stateClass, type MonitorDataItem } from './state'

const { t } = useI18n()
const route = useRoute()

const items = ref<MonitorDataItem[]>([])
const error = ref('')
const loading = ref(false)

const type = computed(() => String(route.params.type ?? ''))

async function load() {
  error.value = ''
  loading.value = true
  try {
    // The list endpoint (not /data/{type}) is used on purpose: it returns the
    // newest sample of every readable server instead of only the first one,
    // and yields an empty array rather than a 404 before the first collection.
    const params = new URLSearchParams({ type: type.value, latest: '1' })
    const serverID = route.query.server_id
    if (serverID) params.set('server_id', String(serverID))
    items.value = (await api.get<MonitorDataItem[] | null>(`/api/monitor/data?${params}`)) ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    loading.value = false
  }
}

watch(() => [route.params.type, route.query.server_id], load, { immediate: true })

/** logText returns the tail payload of a log_* sample, empty otherwise. */
function logText(item: MonitorDataItem): string {
  const data = item.data as Record<string, unknown> | undefined
  return typeof data?.output === 'string' ? data.output : ''
}

/** tableRows returns nested payload rows (disk_usage), empty when flat. */
function tableRows(item: MonitorDataItem): Record<string, unknown>[] {
  const data = item.data as Record<string, unknown> | undefined
  if (!data || typeof data !== 'object') return []
  const values = Object.values(data)
  if (values.length === 0) return []
  if (!values.every((v) => v !== null && typeof v === 'object' && !Array.isArray(v))) return []
  return values as Record<string, unknown>[]
}

/** tableColumns is the union of keys across nested rows, insertion-ordered. */
function tableColumns(rows: Record<string, unknown>[]): string[] {
  const keys: string[] = []
  for (const row of rows) {
    for (const key of Object.keys(row)) {
      if (!key.endsWith('_bytes') && key !== 'use_percent' && !keys.includes(key)) keys.push(key)
    }
  }
  return keys
}

/**
 * serviceState maps the PHP services triple (1 up / 0 down / -1 unused) to a
 * monitor severity so the badge reuses stateClass.
 */
function serviceState(value: unknown): string {
  if (value === 1) return 'ok'
  if (value === 0) return 'error'
  return 'unknown'
}

/** pairs returns the flat key/value entries of a scalar payload. */
function pairs(item: MonitorDataItem): [string, unknown][] {
  const data = item.data as Record<string, unknown> | undefined
  if (!data || typeof data !== 'object') return []
  return Object.entries(data).sort(([a], [b]) => a.localeCompare(b))
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t(`monitor.type.${type}`) }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <p v-if="loading" class="text-sm text-text-muted">{{ t('monitor.loading') }}</p>
    <p
      v-else-if="items.length === 0"
      data-test="monitor-data-empty"
      class="border border-border bg-surface p-5 text-sm text-text-muted"
    >
      {{ t('monitor.no_data') }}
    </p>

    <section
      v-for="item in items"
      :key="`${item.server_id}-${item.created}`"
      class="mb-4 border border-border bg-surface"
      :data-test="`monitor-data-${item.server_id}`"
    >
      <div class="flex items-center justify-between border-b border-border px-4 py-3">
        <span class="font-bold">
          {{ t('monitor.server') }} #{{ item.server_id }} — {{ formatTs(item.created) }}
        </span>
        <span class="border px-2 py-0.5 text-xs font-bold uppercase" :class="stateClass(item.state)">
          {{ t(`monitor.state.${item.state}`) }}
        </span>
      </div>

      <UiAlert
        v-if="item.decode_error"
        variant="danger"
        class="m-3"
        :messages="[t('monitor.decode_error'), item.decode_error]"
      />

      <!-- Log tail -->
      <pre
        v-if="logText(item)"
        data-test="monitor-log"
        class="max-h-[32rem] overflow-auto bg-bg p-3 font-mono text-xs whitespace-pre-wrap"
        >{{ logText(item) }}</pre
      >

      <!-- Nested rows (disk_usage) -->
      <table v-else-if="tableRows(item).length" class="w-full border-collapse text-sm">
        <thead class="bg-thead text-white">
          <tr>
            <th
              v-for="col in tableColumns(tableRows(item))"
              :key="col"
              class="px-3 py-2 text-left text-xs font-bold uppercase"
            >
              {{ col }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in tableRows(item)" :key="i" class="border-t border-border odd:bg-bg">
            <td v-for="col in tableColumns(tableRows(item))" :key="col" class="px-3 py-2">
              {{ row[col] }}
            </td>
          </tr>
        </tbody>
      </table>

      <!-- Flat key/value payload -->
      <table v-else class="w-full border-collapse text-sm">
        <tbody>
          <tr v-for="[key, value] in pairs(item)" :key="key" class="border-t border-border odd:bg-bg">
            <th class="w-1/3 px-3 py-2 text-left font-normal text-text-muted">{{ key }}</th>
            <td class="px-3 py-2" :data-test="`monitor-field-${key}`">
              <span
                v-if="type === 'services'"
                class="border px-1.5 text-xs uppercase"
                :class="stateClass(serviceState(value))"
              >
                {{ t(`monitor.service.${serviceState(value)}`) }}
              </span>
              <template v-else>{{ value }}</template>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

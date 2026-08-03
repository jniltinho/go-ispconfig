<script setup lang="ts">
// Datalog entry detail (port of datalog_view.php): the decoded {old,new}
// payload as a per-field diff. Read-only — sys_datalog is the replication
// journal, so there is deliberately no undo.
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import UiAlert from '../../components/UiAlert.vue'
import { formatTs } from './state'

const props = defineProps<{ id: string }>()

const { t } = useI18n()

interface DatalogDetail {
  datalog_id: number
  server_id: number
  dbtable: string
  dbidx: string
  action: string
  tstamp: number
  user: string
  status: string
  error?: string
  data?: { old?: Record<string, unknown>; new?: Record<string, unknown> }
  data_raw?: string
  decode_error?: string
}

const entry = ref<DatalogDetail | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    entry.value = await api.get<DatalogDetail>(`/api/monitor/datalog/${props.id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

/** fields is the union of old/new keys, so added and removed both show. */
const fields = computed(() => {
  const data = entry.value?.data
  if (!data) return []
  const keys = new Set([...Object.keys(data.old ?? {}), ...Object.keys(data.new ?? {})])
  return [...keys].sort().map((key) => ({
    key,
    old: data.old?.[key],
    new: data.new?.[key],
    changed: JSON.stringify(data.old?.[key]) !== JSON.stringify(data.new?.[key]),
  }))
})
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('monitor.datalog_detail_title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <div v-if="entry" class="border border-border bg-surface">
      <dl class="grid grid-cols-2 gap-x-4 px-4 py-3 text-sm">
        <dt class="text-text-muted">{{ t('monitor.col.datalog_id') }}</dt>
        <dd data-test="datalog-id">{{ entry.datalog_id }}</dd>
        <dt class="text-text-muted">{{ t('monitor.col.dbtable') }}</dt>
        <dd>{{ entry.dbtable }} ({{ entry.dbidx }})</dd>
        <dt class="text-text-muted">{{ t('monitor.col.action') }}</dt>
        <dd>{{ t(`monitor.action.${entry.action}`) }}</dd>
        <dt class="text-text-muted">{{ t('monitor.col.user') }}</dt>
        <dd>{{ entry.user }}</dd>
        <dt class="text-text-muted">{{ t('monitor.col.tstamp') }}</dt>
        <dd>{{ formatTs(entry.tstamp) }}</dd>
        <dt class="text-text-muted">{{ t('monitor.col.status') }}</dt>
        <dd>{{ entry.status }}{{ entry.error ? ` — ${entry.error}` : '' }}</dd>
      </dl>

      <UiAlert
        v-if="entry.decode_error"
        variant="danger"
        class="m-3"
        :messages="[t('monitor.decode_error'), entry.decode_error]"
      />

      <table v-if="fields.length" class="w-full border-collapse border-t border-border text-sm">
        <thead class="bg-thead text-white">
          <tr>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">
              {{ t('monitor.col.field') }}
            </th>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">{{ t('monitor.old') }}</th>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">{{ t('monitor.new') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="f in fields"
            :key="f.key"
            class="border-t border-border odd:bg-bg"
            :class="f.changed ? 'font-bold' : 'text-text-muted'"
            :data-test="`datalog-field-${f.key}`"
          >
            <td class="px-3 py-2">{{ f.key }}</td>
            <td class="px-3 py-2">{{ f.old }}</td>
            <td class="px-3 py-2">{{ f.new }}</td>
          </tr>
        </tbody>
      </table>
      <pre
        v-else-if="entry.data_raw"
        class="max-h-96 overflow-auto border-t border-border bg-bg p-3 font-mono text-xs whitespace-pre-wrap"
        >{{ entry.data_raw }}</pre
      >

      <div class="border-t border-border bg-bg px-4 py-3">
        <RouterLink to="/monitor/datalog" class="btn btn-default px-6 no-underline">
          {{ t('monitor.back_to_list') }}
        </RouterLink>
      </div>
    </div>
  </div>
</template>

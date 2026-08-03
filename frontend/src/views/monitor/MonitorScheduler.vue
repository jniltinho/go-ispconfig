<script setup lang="ts">
// Scheduler jobs (admin): the sys_config mirror the daemon writes at job
// registration and after every run. Read-only — jobs are registered in code.
import { onMounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import UiAlert from '../../components/UiAlert.vue'
import { stateClass } from './state'

const { t } = useI18n()

interface SchedulerJob {
  name: string
  spec: string
  last_run: string
  status: string
  next_run: string
}

const jobs = ref<SchedulerJob[]>([])
const error = ref('')

onMounted(async () => {
  try {
    jobs.value = (await api.get<SchedulerJob[] | null>('/api/system/scheduler')) ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

/** localTime renders an RFC3339 instant in the browser locale. */
function localTime(value: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

/** A status other than "ok" (including "never run") is not a success. */
function statusState(status: string): string {
  if (status === 'ok') return 'ok'
  return status ? 'error' : 'unknown'
}
</script>

<template>
  <div>
    <h1 class="mb-3 text-lg font-bold">{{ t('monitor.scheduler_title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <div class="overflow-x-auto border border-border bg-surface">
      <table class="w-full border-collapse text-sm">
        <thead class="bg-thead text-white">
          <tr>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">
              {{ t('monitor.col.job') }}
            </th>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">
              {{ t('monitor.col.spec') }}
            </th>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">
              {{ t('monitor.col.last_run') }}
            </th>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">
              {{ t('monitor.col.next_run') }}
            </th>
            <th class="px-3 py-2 text-left text-xs font-bold uppercase">
              {{ t('monitor.col.status') }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="jobs.length === 0" data-test="scheduler-empty">
            <td colspan="5" class="px-3 py-10 text-center text-sm text-text-muted">
              {{ t('monitor.no_data') }}
            </td>
          </tr>
          <tr
            v-for="job in jobs"
            :key="job.name"
            class="border-t border-border odd:bg-bg"
            :data-test="`scheduler-job-${job.name}`"
          >
            <td class="px-3 py-2">{{ job.name }}</td>
            <td class="px-3 py-2 font-mono text-xs">{{ job.spec || '-' }}</td>
            <td class="px-3 py-2">{{ localTime(job.last_run) }}</td>
            <td class="px-3 py-2">{{ localTime(job.next_run) }}</td>
            <td class="px-3 py-2">
              <span
                class="border px-1.5 text-xs uppercase"
                :class="stateClass(statusState(job.status))"
              >
                {{ job.status || t('monitor.never_run') }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

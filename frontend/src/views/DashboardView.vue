<script setup lang="ts">
// Dashboard as ISPConfig3 dashlets: one card per enabled module
// (dashlet background, large module icon, title, full-width button).
import { onMounted, ref } from 'vue'
import { CircleHelp } from 'lucide-vue-next'
import { moduleIcons } from '../icons'
import { modules } from '../modules'
import { useI18n } from '../i18n'
import { api } from '../api'
import { useAuthStore } from '../stores/auth'
import { stateClass, type ServerState } from './monitor/state'
import MetricChart from '../components/MetricChart.vue'

const { t } = useI18n()
const auth = useAuthStore()

// Every module except the dashboard itself becomes a dashlet.
const dashlets = modules.filter((mod) => mod.id !== 'dashboard')

// Monitor summary strip. Every call is optional: without the monitor module
// the API answers 403 and the strip simply stays hidden, so the dashboard
// never depends on monitoring being reachable.
const worstState = ref('')
const pendingJobs = ref<number | null>(null)
const failedJobs = ref<number | null>(null)

/** SysUsage is the decoded sys_usage payload: rolling series, oldest first. */
interface SysUsage {
  load?: number[]
  mem?: number[]
  time?: string[]
  net?: { rx: number; tx: number }[]
}

/** One System metrics block per monitored server (legacy dashlet_metrics). */
const metrics = ref<{ serverId: number; usage: SysUsage }[]>([])

onMounted(async () => {
  try {
    const rows =
      (await api.get<{ server_id: number; data?: SysUsage }[] | null>(
        '/api/monitor/data?type=sys_usage',
      )) ?? []
    metrics.value = rows
      .filter((r) => r.data?.load?.length)
      .map((r) => ({ serverId: r.server_id, usage: r.data as SysUsage }))
  } catch {
    // No monitor module (403) or no samples yet — section stays hidden.
  }
  try {
    const states = (await api.get<ServerState[] | null>('/api/monitor/state')) ?? []
    if (states.length > 0) {
      // The aggregate already folds severity per server; the dashlet shows
      // the worst one, ranked by the same order the API uses.
      const rank = ['no_state', 'ok', 'unknown', 'info', 'warning', 'critical', 'error']
      worstState.value = states.reduce(
        (worst, s) => (rank.indexOf(s.state) > rank.indexOf(worst) ? s.state : worst),
        'no_state',
      )
    }
  } catch {
    // No monitor module (403) or no data — leave the tile out.
  }
  try {
    pendingJobs.value = (await api.get<{ count: number }>('/api/monitor/jobqueue/count')).count
  } catch {
    // Same: optional tile.
  }
  try {
    const jobs = await api.get<{ status: string }[] | null>('/api/system/scheduler')
    failedJobs.value = (jobs ?? []).filter((j) => j.status && j.status !== 'ok').length
  } catch {
    // Admin-only endpoint; hidden for everyone else.
  }
})
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('dashboard.welcome', { username: auth.username }) }}</h1>

    <ul
      v-if="worstState || pendingJobs !== null || failedJobs !== null"
      class="mb-4 grid grid-cols-2 gap-4 md:grid-cols-4"
    >
      <li
        v-if="worstState"
        class="border border-border bg-dashlet p-4"
        data-test="dashlet-monitor-state"
      >
        <p class="text-xs font-bold uppercase text-text-muted">{{ t('dashboard.monitor_state') }}</p>
        <span
          class="mt-2 inline-block border px-2 py-0.5 text-sm font-bold uppercase"
          :class="stateClass(worstState)"
        >
          {{ t(`monitor.state.${worstState}`) }}
        </span>
      </li>
      <li
        v-if="pendingJobs !== null"
        class="border border-border bg-dashlet p-4"
        data-test="dashlet-monitor-jobqueue"
      >
        <p class="text-xs font-bold uppercase text-text-muted">
          {{ t('dashboard.monitor_jobqueue') }}
        </p>
        <p class="mt-2 text-2xl font-bold">{{ pendingJobs }}</p>
      </li>
      <li
        v-if="failedJobs !== null"
        class="border border-border bg-dashlet p-4"
        data-test="dashlet-monitor-failed-jobs"
      >
        <p class="text-xs font-bold uppercase text-text-muted">
          {{ t('dashboard.monitor_failed_jobs') }}
        </p>
        <p class="mt-2 text-2xl font-bold">{{ failedJobs }}</p>
      </li>
    </ul>

    <template v-if="metrics.length">
      <h2 class="page-title">{{ t('dashboard.metrics') }}</h2>
      <div
        v-for="m in metrics"
        :key="m.serverId"
        class="mb-4 border border-border bg-dashlet p-4"
        :data-test="`dashlet-metrics-${m.serverId}`"
      >
        <p v-if="metrics.length > 1" class="mb-2 text-xs font-bold uppercase text-text-muted">
          {{ t('monitor.server_id') }} {{ m.serverId }}
        </p>
        <div class="grid gap-3 md:grid-cols-2">
          <MetricChart
            :label="t('dashboard.metrics.load')"
            :values="m.usage.load ?? []"
            :times="m.usage.time"
          />
          <MetricChart
            :label="t('dashboard.metrics.memory')"
            :values="m.usage.mem ?? []"
            :times="m.usage.time"
          />
          <MetricChart
            :label="t('dashboard.metrics.net_in')"
            :values="(m.usage.net ?? []).map((p) => p.rx)"
            :times="m.usage.time"
          />
          <MetricChart
            :label="t('dashboard.metrics.net_out')"
            :values="(m.usage.net ?? []).map((p) => p.tx)"
            :times="m.usage.time"
          />
        </div>
      </div>
    </template>

    <h2 class="page-title">{{ t('dashboard.available_modules') }}</h2>

    <ul class="grid grid-cols-2 gap-4 md:grid-cols-4">
      <li
        v-for="mod in dashlets"
        :key="mod.id"
        class="flex flex-col gap-3 border border-border bg-dashlet p-4"
        :data-test="`dashlet-${mod.id}`"
      >
        <!-- Legacy dashlet head: icon left, title right on one row. -->
        <div class="flex items-center gap-3">
          <component
            :is="moduleIcons[mod.id] ?? CircleHelp"
            :size="38"
            :stroke-width="1.25"
            class="shrink-0 text-text"
          />
          <span class="flex-1 text-center text-base font-bold">{{ t(`module.${mod.id}`) }}</span>
        </div>
        <RouterLink :to="mod.path" class="btn btn-default w-full no-underline">
          {{ t('dashboard.open_module', { module: t(`module.${mod.id}`) }) }}
        </RouterLink>
      </li>
    </ul>
  </div>
</template>

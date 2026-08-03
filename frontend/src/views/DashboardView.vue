<script setup lang="ts">
// Dashboard as ISPConfig3 dashlets: one card per enabled module
// (dashlet background, large module icon, title, full-width button).
import { onMounted, ref } from 'vue'
import { CircleHelp } from 'lucide-vue-next'
import { moduleIcons } from '../icons'
import { modules } from '../modules'
import { useI18n } from '../i18n'
import { api } from '../api'
import { stateClass, type ServerState } from './monitor/state'

const { t } = useI18n()

// Every module except the dashboard itself becomes a dashlet.
const dashlets = modules.filter((mod) => mod.id !== 'dashboard')

// Monitor summary strip. Every call is optional: without the monitor module
// the API answers 403 and the strip simply stays hidden, so the dashboard
// never depends on monitoring being reachable.
const worstState = ref('')
const pendingJobs = ref<number | null>(null)
const failedJobs = ref<number | null>(null)

onMounted(async () => {
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
    <h1 class="page-title">{{ t('module.dashboard') }}</h1>

    <ul
      v-if="worstState || pendingJobs !== null || failedJobs !== null"
      class="mb-4 grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-4"
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

    <ul class="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-4">
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
          <span class="ml-auto text-base font-bold">{{ t(`module.${mod.id}`) }}</span>
        </div>
        <RouterLink :to="mod.path" class="btn btn-default w-full no-underline">
          {{ t('dashboard.open_module', { module: t(`module.${mod.id}`) }) }}
        </RouterLink>
      </li>
    </ul>
  </div>
</template>

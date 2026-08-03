<script setup lang="ts">
// Dashboard as ISPConfig3 dashlets, in the legacy dashboard.php column
// order: modules, metrics, quota, mailquota, databasequota (left column)
// then limits (right column). Every dashlet is a DashletCard box.
// invoices/customer/products/shop are legacy billing/shop plugin dashlets
// with no Go API behind them, so they are deliberately left out.
import { onMounted, ref } from 'vue'
import { CircleHelp } from 'lucide-vue-next'
import { moduleIcons } from '../icons'
import { modules } from '../modules'
import { useI18n } from '../i18n'
import { api } from '../api'
import { useAuthStore } from '../stores/auth'
import { stateClass, type ServerState } from './monitor/state'
import DashletCard from '../components/DashletCard.vue'
import MetricChart from '../components/MetricChart.vue'
import QuotaBlock, { type QuotaRow } from '../components/QuotaBlock.vue'
import LimitBlock, { type LimitRow } from '../components/LimitBlock.vue'

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

// Quota dashlets (legacy quota/mailquota/databasequota). All three read the
// matching monitor_data type; each stays hidden when nothing was collected.
const hdQuota = ref<QuotaRow[]>([])
const mailQuota = ref<QuotaRow[]>([])
const dbQuota = ref<QuotaRow[]>([])

// Account limits dashlet (legacy dashlet_limits). Admins get unlimited: the
// legacy dashlet then prints nothing but "unlimited", so we hide the block.
const limits = ref<LimitRow[]>([])
const limitsUnlimited = ref(false)

/** quotaRows reads one monitor_data type and maps it to the block shape. */
async function quotaRows<T>(type: string, map: (row: T) => QuotaRow): Promise<QuotaRow[]> {
  try {
    const res = await api.get<{ data?: T[] }>(`/api/monitor/data/${type}`)
    return (res.data ?? []).map(map)
  } catch {
    // No monitor module (403) or no sample yet — block stays hidden.
    return []
  }
}

onMounted(async () => {
  // harddisk_quota is reported in KB, the other two already in bytes.
  hdQuota.value = await quotaRows<{ domain: string; used: number; soft: number }>(
    'harddisk_quota',
    (r) => ({ name: r.domain, used: r.used * 1024, total: r.soft * 1024 }),
  )
  mailQuota.value = await quotaRows<{ email: string; used: number; quota: number }>(
    'email_quota',
    (r) => ({ name: r.email, used: r.used, total: r.quota }),
  )
  dbQuota.value = await quotaRows<{ database_name: string; size: number; quota: number }>(
    'database_size',
    (r) => ({ name: r.database_name, used: r.size, total: r.quota }),
  )
  try {
    const res = await api.get<{ unlimited: boolean; limits: LimitRow[] }>('/api/monitor/limits')
    limitsUnlimited.value = res.unlimited
    limits.value = res.limits ?? []
  } catch {
    // Unauthenticated edge or backend without the endpoint: block stays out.
  }
  try {
    const rows =
      (await api.get<{ server_id: number; data?: SysUsage }[] | null>(
        '/api/monitor/data?type=sys_usage',
      )) ?? []
    metrics.value = rows
      // A server with a sys_usage row but no samples still gets a block; the
      // charts render their own "no data" state.
      .filter((r) => r.data)
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
      class="mb-3 grid grid-cols-2 gap-3 md:grid-cols-4"
    >
      <li
        v-if="worstState"
        class="border border-border bg-dashlet p-3"
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
        class="border border-border bg-dashlet p-3"
        data-test="dashlet-monitor-jobqueue"
      >
        <p class="text-xs font-bold uppercase text-text-muted">
          {{ t('dashboard.monitor_jobqueue') }}
        </p>
        <p class="mt-2 text-2xl font-bold">{{ pendingJobs }}</p>
      </li>
      <li
        v-if="failedJobs !== null"
        class="border border-border bg-dashlet p-3"
        data-test="dashlet-monitor-failed-jobs"
      >
        <p class="text-xs font-bold uppercase text-text-muted">
          {{ t('dashboard.monitor_failed_jobs') }}
        </p>
        <p class="mt-2 text-2xl font-bold">{{ failedJobs }}</p>
      </li>
    </ul>

    <!-- Legacy dashlet order (dashboard.php $default_leftcol_dashlets then
         $default_rightcol_dashlets, restricted to the dashlets that ship in
         dashboard/dashlets/): modules, metrics, quota, mailquota,
         databasequota, then limits from the right column. -->
    <DashletCard :title="t('dashboard.available_modules')" class="mb-3">
      <ul class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
        <li
          v-for="mod in dashlets"
          :key="mod.id"
          class="flex flex-col gap-2 border border-border bg-surface p-2"
          :data-test="`dashlet-${mod.id}`"
        >
          <!-- Legacy dashlet head: icon left, title right on one row. -->
          <div class="flex items-center gap-2">
            <component
              :is="moduleIcons[mod.id] ?? CircleHelp"
              :size="28"
              :stroke-width="1.25"
              class="shrink-0 text-text"
            />
            <span class="flex-1 text-center text-sm font-bold">{{ t(`module.${mod.id}`) }}</span>
          </div>
          <RouterLink :to="mod.path" class="btn btn-default mt-auto w-full no-underline">
            {{ t('dashboard.open_module', { module: t(`module.${mod.id}`) }) }}
          </RouterLink>
        </li>
      </ul>
    </DashletCard>

    <DashletCard
      v-for="m in metrics"
      :key="m.serverId"
      :title="
        metrics.length > 1
          ? `${t('dashboard.metrics')} — ${t('monitor.server_id')} ${m.serverId}`
          : t('dashboard.metrics')
      "
      class="mb-3"
      :data-test="`dashlet-metrics-${m.serverId}`"
    >
      <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
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
    </DashletCard>

    <!-- quota, mailquota, databasequota (left column) then limits (right). -->
    <div class="mb-3 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      <QuotaBlock v-if="hdQuota.length" :title="t('dashboard.quota.harddisk')" :rows="hdQuota" />
      <QuotaBlock v-if="mailQuota.length" :title="t('dashboard.quota.mailbox')" :rows="mailQuota" />
      <QuotaBlock v-if="dbQuota.length" :title="t('dashboard.quota.database')" :rows="dbQuota" />
      <LimitBlock
        v-if="limitsUnlimited || limits.length"
        :rows="limits"
        :unlimited="limitsUnlimited"
      />
    </div>
  </div>
</template>

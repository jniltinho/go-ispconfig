<script setup lang="ts">
// Migrate-from-ISPConfig3 wizard (change add-legacy-migration, group 6):
// connect → inventory → dry-run → execute → report over the admin-only
// /api/system/migration/* endpoints. Credentials go to the backend once
// on connect and never come back in any response.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()

// ---- API payload shapes (mirrors internal/api/migration.go) ----

interface MigrationError {
  error: string
  fault_code?: string
  missing_functions?: string[]
}

interface ConnectResponse {
  servers: Record<string, string>[]
  multi_server: boolean
  insecure: boolean
  plain_http: boolean
}

interface Inventory {
  clients: number
  web_domains: number
  web_folders: number
  web_folder_users: number
  dns_zones: number
  dns_records: number
  dns_slave_zones: number
  dns_templates: number
  servers: Record<string, string>[]
  multi_server: boolean
}

interface EntityCount {
  created: number
  updated: number
  skipped: number
  conflicts: number
}

interface PlanItem {
  table: string
  key: string
  action: string
  reason?: string
}

interface PlanResponse {
  counts: Record<string, EntityCount>
  conflicts: PlanItem[]
  warnings: string[]
  reset_required: string[]
}

interface Progress {
  entity: string
  done: number
  total: number
}

interface Report {
  counts: Record<string, EntityCount>
  conflicts: PlanItem[]
  reset_required: string[]
  warnings: string[]
  rsync_suggestions: string[]
  operational_order: string[]
}

interface Status {
  state: string
  legacy_url?: string
  insecure?: boolean
  plain_http?: boolean
  multi_server?: boolean
  inventory?: Inventory
  progress?: Record<string, Progress>
  error?: string
  report?: Report
}

// ---- wizard state ----

type Step = 'connect' | 'inventory' | 'dryrun' | 'execute' | 'report'
const step = ref<Step>('connect')
const steps: Step[] = ['connect', 'inventory', 'dryrun', 'execute', 'report']

const busy = ref(false)
const failure = ref<MigrationError | null>(null)

// Connect step.
const form = ref({ url: '', username: '', password: '', insecure: false })
const connectInfo = ref<ConnectResponse | null>(null)

// Inventory step.
const inventory = ref<Inventory | null>(null)
const selection = ref({ clients: true, sites: true, dns: true })
const targetServerId = ref<number>(0)
const confirmMapAll = ref(false)
const orphansToAdmin = ref(false)

// Deselecting everything must not silently mean "import everything".
const nothingSelected = computed(
  () => !selection.value.clients && !selection.value.sites && !selection.value.dns,
)

// Long conflict lists are capped in the UI; the full list is in the API
// response and the CLI output.
const conflictLimit = 100

// Go marshals empty slices as null; normalize them once at the fetch
// boundary so the template can use .length safely.
function planOf(p: PlanResponse): PlanResponse {
  return {
    counts: p.counts ?? {},
    conflicts: p.conflicts ?? [],
    warnings: p.warnings ?? [],
    reset_required: p.reset_required ?? [],
  }
}

function reportOf(r: Report): Report {
  return {
    counts: r.counts ?? {},
    conflicts: r.conflicts ?? [],
    reset_required: r.reset_required ?? [],
    warnings: r.warnings ?? [],
    rsync_suggestions: r.rsync_suggestions ?? [],
    operational_order: r.operational_order ?? [],
  }
}

// failureOf extracts the wizard error payload from an ApiError.
function failureOf(e: unknown): MigrationError {
  if (e instanceof ApiError && e.body) {
    try {
      const parsed = JSON.parse(e.body) as MigrationError
      if (parsed.error) return parsed
    } catch {
      // fall through to the generic message
    }
  }
  return { error: e instanceof ApiError ? t(e.key) : t('error.request_failed') }
}

async function testConnection() {
  busy.value = true
  failure.value = null
  try {
    connectInfo.value = await api.post<ConnectResponse>('/api/system/migration/connect', {
      url: form.value.url,
      username: form.value.username,
      password: form.value.password,
      insecure: form.value.insecure,
    })
  } catch (e) {
    connectInfo.value = null
    failure.value = failureOf(e)
  } finally {
    busy.value = false
  }
}

async function loadInventory() {
  busy.value = true
  failure.value = null
  try {
    inventory.value = await api.get<Inventory>('/api/system/migration/inventory')
    step.value = 'inventory'
  } catch (e) {
    failure.value = failureOf(e)
  } finally {
    busy.value = false
  }
}

// runOptions builds the shared dry-run/execute payload.
function runOptions() {
  return {
    selection: { ...selection.value },
    target_server_id: targetServerId.value || 0,
    assign_orphan_zones_to_admin: orphansToAdmin.value,
    confirm_map_all_to_local_server: confirmMapAll.value,
  }
}

onMounted(reattach)
onUnmounted(stopProgress)

// ---- dry-run step ----

const plan = ref<PlanResponse | null>(null)

async function runDryRun() {
  busy.value = true
  failure.value = null
  try {
    plan.value = planOf(await api.post<PlanResponse>('/api/system/migration/dry-run', runOptions()))
    step.value = 'dryrun'
  } catch (e) {
    failure.value = failureOf(e)
  } finally {
    busy.value = false
  }
}

// ---- execute step: SSE with polling fallback, reattach via status ----

const progress = ref<Record<string, Progress>>({})
const runError = ref('')
const report = ref<Report | null>(null)
let source: EventSource | null = null
let pollTimer: number | null = null

function stopProgress() {
  source?.close()
  source = null
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function applyStatus(status: Status) {
  if (status.progress) progress.value = status.progress
  if (status.state === 'done') {
    report.value = status.report ? reportOf(status.report) : null
    runError.value = ''
    stopProgress()
    step.value = 'report'
  } else if (status.state === 'failed') {
    runError.value = status.error ?? 'failed'
    stopProgress()
  }
}

function startPolling() {
  if (pollTimer !== null) return
  pollTimer = window.setInterval(async () => {
    try {
      applyStatus(await api.get<Status>('/api/system/migration/status'))
    } catch {
      // transient; keep polling
    }
  }, 2000)
}

function watchProgress() {
  stopProgress()
  try {
    source = new EventSource('/api/system/migration/progress')
    source.addEventListener('progress', (ev) => {
      const p = JSON.parse((ev as MessageEvent).data) as Progress
      progress.value = { ...progress.value, [p.entity]: p }
    })
    source.addEventListener('status', (ev) => {
      applyStatus(JSON.parse((ev as MessageEvent).data) as Status)
    })
    source.onerror = () => {
      // Buffering/broken proxy: fall back to polling the status snapshot.
      stopProgress()
      startPolling()
    }
  } catch {
    startPolling()
  }
}

async function execute() {
  busy.value = true
  failure.value = null
  runError.value = ''
  try {
    await api.post<Status>('/api/system/migration/execute', runOptions())
    progress.value = {}
    step.value = 'execute'
    watchProgress()
  } catch (e) {
    failure.value = failureOf(e)
  } finally {
    busy.value = false
  }
}

// reattach restores a running/finished wizard after a page reload.
async function reattach() {
  try {
    const status = await api.get<Status>('/api/system/migration/status')
    if (status.state === 'running') {
      if (status.inventory) inventory.value = status.inventory
      progress.value = status.progress ?? {}
      step.value = 'execute'
      watchProgress()
    } else if (status.state === 'failed') {
      runError.value = status.error ?? 'failed'
      progress.value = status.progress ?? {}
      step.value = 'execute'
    } else if (status.state === 'done' && status.report) {
      report.value = reportOf(status.report)
      step.value = 'report'
    }
  } catch {
    // idle wizard: stay on the connect step
  }
}

// ---- report step ----

interface ResetToken {
  username: string
  token: string
}
const resetTokens = ref<ResetToken[]>([])

async function generateResetTokens() {
  busy.value = true
  failure.value = null
  try {
    resetTokens.value = await api.post<ResetToken[]>('/api/system/migration/reset-passwords')
  } catch (e) {
    failure.value = failureOf(e)
  } finally {
    busy.value = false
  }
}

// startOver resets the wizard to the connection step (the backend closes
// the legacy session when a run finishes, so a fresh connect is required).
function startOver() {
  stopProgress()
  connectInfo.value = null
  inventory.value = null
  plan.value = null
  report.value = null
  resetTokens.value = []
  progress.value = {}
  runError.value = ''
  failure.value = null
  confirmMapAll.value = false
  step.value = 'connect'
}

const tokensCopied = ref(false)

// copyTokens puts every username/token pair on the clipboard.
async function copyTokens() {
  const text = resetTokens.value.map((tok) => `${tok.username}\t${tok.token}`).join('\n')
  try {
    await navigator.clipboard.writeText(text)
    tokensCopied.value = true
  } catch {
    // Clipboard unavailable (permissions/http): tokens stay visible.
  }
}

// entityRows flattens a counts map for rendering.
function entityRows(counts: Record<string, EntityCount>) {
  return Object.keys(counts)
    .sort()
    .map((table) => ({ table, ...counts[table] }))
}

const inputClass =
  'w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link'
const buttonClass = 'bg-success px-8 py-1.5 text-xs font-bold text-white hover:opacity-90 disabled:opacity-50'
const secondaryButtonClass = 'border border-border bg-surface px-6 py-1.5 text-xs font-bold hover:bg-info'
</script>

<template>
  <div>
    <h1 class="mb-3 text-lg font-bold">{{ t('migration.title') }}</h1>

    <!-- Step indicator -->
    <ol class="mb-4 flex gap-2 text-xs">
      <li
        v-for="(s, i) in steps"
        :key="s"
        class="border px-3 py-1"
        :class="s === step ? 'border-link bg-info font-bold' : 'border-border bg-surface text-text-muted'"
      >
        {{ i + 1 }}. {{ t(`migration.step.${s}`) }}
      </li>
    </ol>

    <!-- Wizard-level failure -->
    <div
      v-if="failure"
      data-test="migration-error"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
    >
      <p>{{ failure.error }}</p>
      <p v-if="failure.fault_code" class="mt-1 text-xs">
        {{ t('migration.fault_code') }}: {{ failure.fault_code }}
      </p>
      <div v-if="failure.missing_functions?.length" class="mt-1">
        <p class="text-xs font-bold">{{ t('migration.missing_grants') }}:</p>
        <ul class="ml-4 list-disc text-xs">
          <li v-for="fn in failure.missing_functions" :key="fn">{{ fn }}</li>
        </ul>
      </div>
    </div>

    <!-- Step 1: connection -->
    <form
      v-if="step === 'connect'"
      class="max-w-2xl border border-border bg-surface"
      @submit.prevent="testConnection"
    >
      <div class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="mig-url" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('migration.connect.url') }}
          </label>
          <input id="mig-url" v-model="form.url" type="text" required
            placeholder="https://legacy.example.com:8080" :class="inputClass" data-test="mig-url" />
        </div>
        <div class="flex items-start gap-4">
          <label for="mig-user" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('migration.connect.username') }}
          </label>
          <input id="mig-user" v-model="form.username" type="text" required :class="inputClass" data-test="mig-user" />
        </div>
        <div class="flex items-start gap-4">
          <label for="mig-pass" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('migration.connect.password') }}
          </label>
          <input id="mig-pass" v-model="form.password" type="password" required
            autocomplete="new-password" :class="inputClass" data-test="mig-pass" />
        </div>
        <div class="flex items-center gap-4">
          <span class="w-48 shrink-0 text-right text-sm font-semibold after:content-[':']">
            {{ t('migration.connect.insecure') }}
          </span>
          <label class="flex items-center gap-2 text-sm">
            <input v-model="form.insecure" type="checkbox" data-test="mig-insecure" />
            <span>{{ t('migration.connect.insecure_label') }}</span>
          </label>
        </div>
        <p v-if="form.insecure" class="ml-52 max-w-md border border-danger-border bg-danger px-3 py-2 text-xs text-danger-text">
          {{ t('migration.connect.insecure_warning') }}
        </p>

        <!-- Connection test result -->
        <div v-if="connectInfo" data-test="mig-panel-info"
          class="ml-52 max-w-md space-y-1 border border-border bg-bg px-3 py-2 text-sm">
          <p class="font-bold">{{ t('migration.connect.ok') }}</p>
          <p v-for="s in connectInfo.servers" :key="s.server_id" class="text-xs">
            {{ t('migration.connect.server') }}: {{ s.server_name }} (id {{ s.server_id }})
          </p>
          <p v-if="connectInfo.plain_http" class="text-xs text-danger-text">
            {{ t('migration.warn.plain_http') }}
          </p>
          <p v-if="connectInfo.multi_server" class="text-xs text-danger-text">
            {{ t('migration.warn.multi_server') }}
          </p>
        </div>
      </div>

      <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
        <button type="submit" :disabled="busy" :class="secondaryButtonClass" data-test="mig-test">
          {{ t('migration.connect.test') }}
        </button>
        <button type="button" :disabled="busy || !connectInfo" :class="buttonClass"
          data-test="mig-to-inventory" @click="loadInventory">
          {{ t('migration.next_inventory') }}
        </button>
      </div>
    </form>

    <!-- Step 2: inventory -->
    <div v-if="step === 'inventory' && inventory" class="max-w-2xl border border-border bg-surface">
      <div class="space-y-4 px-3 py-6">
        <table class="w-full max-w-md text-sm" data-test="mig-inventory">
          <tbody>
            <tr v-for="row in [
              { key: 'clients', value: inventory.clients },
              { key: 'web_domains', value: inventory.web_domains },
              { key: 'web_folders', value: inventory.web_folders },
              { key: 'web_folder_users', value: inventory.web_folder_users },
              { key: 'dns_zones', value: inventory.dns_zones },
              { key: 'dns_records', value: inventory.dns_records },
              { key: 'dns_slave_zones', value: inventory.dns_slave_zones },
              { key: 'dns_templates', value: inventory.dns_templates },
            ]" :key="row.key" class="border-b border-border">
              <td class="py-1">{{ t(`migration.inventory.${row.key}`) }}</td>
              <td class="py-1 text-right font-mono">{{ row.value }}</td>
            </tr>
          </tbody>
        </table>

        <div class="space-y-1 text-sm">
          <p class="font-semibold">{{ t('migration.inventory.select') }}:</p>
          <label class="flex items-center gap-2">
            <input v-model="selection.clients" type="checkbox" data-test="mig-sel-clients" />
            {{ t('migration.inventory.sel_clients') }}
          </label>
          <label class="flex items-center gap-2">
            <input v-model="selection.sites" type="checkbox" data-test="mig-sel-sites" />
            {{ t('migration.inventory.sel_sites') }}
          </label>
          <label class="flex items-center gap-2">
            <input v-model="selection.dns" type="checkbox" data-test="mig-sel-dns" />
            {{ t('migration.inventory.sel_dns') }}
          </label>
          <label v-if="selection.dns" class="ml-6 flex items-center gap-2 text-xs">
            <input v-model="orphansToAdmin" type="checkbox" data-test="mig-orphans" />
            {{ t('migration.inventory.orphans_to_admin') }}
          </label>
        </div>

        <div class="flex items-center gap-4 text-sm">
          <label for="mig-target" class="font-semibold">{{ t('migration.inventory.target_server') }}:</label>
          <input id="mig-target" v-model.number="targetServerId" type="number" min="0"
            class="w-24 border border-border bg-surface px-2 py-1 text-sm outline-none" />
          <span class="text-xs text-text-muted">{{ t('migration.inventory.target_server_hint') }}</span>
        </div>

        <!-- Multi-server guard -->
        <div v-if="inventory.multi_server" data-test="mig-multiserver"
          class="space-y-2 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text">
          <p class="font-bold">{{ t('migration.multi_server.title') }}</p>
          <p class="text-xs">{{ t('migration.multi_server.text') }}</p>
          <ul class="ml-4 list-disc text-xs">
            <li v-for="s in inventory.servers" :key="s.server_id">
              {{ s.server_name }} (id {{ s.server_id }})
            </li>
          </ul>
          <label class="flex items-center gap-2 text-xs font-bold">
            <input v-model="confirmMapAll" type="checkbox" data-test="mig-confirm-multi" />
            {{ t('migration.multi_server.confirm') }}
          </label>
        </div>
      </div>

      <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
        <button type="button" :class="secondaryButtonClass" @click="step = 'connect'">
          {{ t('migration.back') }}
        </button>
        <button type="button" :disabled="busy || nothingSelected || (inventory.multi_server && !confirmMapAll)"
          :class="buttonClass" data-test="mig-to-dryrun" @click="runDryRun">
          {{ t('migration.next_dryrun') }}
        </button>
      </div>
    </div>

    <!-- Step 3: dry-run plan -->
    <div v-if="step === 'dryrun' && plan" class="max-w-3xl border border-border bg-surface">
      <div class="space-y-4 px-3 py-6">
        <table class="w-full text-sm" data-test="mig-plan">
          <thead>
            <tr class="border-b border-border text-left text-xs uppercase text-text-muted">
              <th class="py-1">{{ t('migration.plan.entity') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.create') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.update') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.skip') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.conflict') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in entityRows(plan.counts)" :key="row.table" class="border-b border-border">
              <td class="py-1 font-mono">{{ row.table }}</td>
              <td class="py-1 text-right">{{ row.created }}</td>
              <td class="py-1 text-right">{{ row.updated }}</td>
              <td class="py-1 text-right">{{ row.skipped }}</td>
              <td class="py-1 text-right" :class="{ 'font-bold text-danger-text': row.conflicts }">
                {{ row.conflicts }}
              </td>
            </tr>
          </tbody>
        </table>

        <div v-if="plan.conflicts.length" data-test="mig-conflicts"
          class="border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text">
          <p class="font-bold">{{ t('migration.plan.conflicts_title', { n: plan.conflicts.length }) }}</p>
          <p class="text-xs">{{ t('migration.plan.conflicts_note') }}</p>
          <ul class="ml-4 mt-1 list-disc text-xs">
            <li v-for="(c, i) in plan.conflicts.slice(0, conflictLimit)" :key="i">
              <span class="font-mono">{{ c.table }} {{ c.key }}</span>: {{ c.reason }}
            </li>
          </ul>
          <p v-if="plan.conflicts.length > conflictLimit" class="mt-1 text-xs">
            {{ t('migration.plan.conflicts_more', { n: plan.conflicts.length - conflictLimit }) }}
          </p>
        </div>

        <ul v-if="plan.warnings.length" class="space-y-1 text-xs">
          <li v-for="(w, i) in plan.warnings" :key="i" class="border border-border bg-bg px-2 py-1">
            {{ w }}
          </li>
        </ul>

        <p v-if="plan.reset_required.length" class="text-xs text-text-muted">
          {{ t('migration.plan.reset_note', { n: plan.reset_required.length }) }}
        </p>
      </div>

      <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
        <button type="button" :class="secondaryButtonClass" @click="step = 'inventory'">
          {{ t('migration.back') }}
        </button>
        <button type="button" :disabled="busy" :class="buttonClass" data-test="mig-execute" @click="execute">
          {{ t('migration.execute') }}
        </button>
      </div>
    </div>

    <!-- Step 4: execution progress -->
    <div v-if="step === 'execute'" class="max-w-2xl border border-border bg-surface px-3 py-6">
      <p class="mb-3 text-sm font-semibold">{{ t('migration.execute.running') }}</p>
      <div class="space-y-2" data-test="mig-progress">
        <div v-for="p in Object.values(progress)" :key="p.entity" class="text-sm">
          <div class="mb-0.5 flex justify-between font-mono text-xs">
            <span>{{ p.entity }}</span>
            <span>{{ p.done }}/{{ p.total }}</span>
          </div>
          <div class="h-2 w-full border border-border bg-bg">
            <div class="h-full bg-success" :style="{ width: `${p.total ? (100 * p.done) / p.total : 0}%` }" />
          </div>
        </div>
      </div>
      <p v-if="runError" data-test="mig-run-error"
        class="mt-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text">
        {{ t('migration.execute.failed') }}: {{ runError }}
      </p>
      <div v-if="runError" class="mt-3">
        <button type="button" :class="secondaryButtonClass" data-test="mig-start-over" @click="startOver">
          {{ t('migration.start_over') }}
        </button>
      </div>
    </div>

    <!-- Step 5: final report -->
    <div v-if="step === 'report' && report" class="max-w-3xl border border-border bg-surface">
      <div class="space-y-5 px-3 py-6" data-test="mig-report">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-border text-left text-xs uppercase text-text-muted">
              <th class="py-1">{{ t('migration.plan.entity') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.create') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.update') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.skip') }}</th>
              <th class="py-1 text-right">{{ t('migration.plan.conflict') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in entityRows(report.counts)" :key="row.table" class="border-b border-border">
              <td class="py-1 font-mono">{{ row.table }}</td>
              <td class="py-1 text-right">{{ row.created }}</td>
              <td class="py-1 text-right">{{ row.updated }}</td>
              <td class="py-1 text-right">{{ row.skipped }}</td>
              <td class="py-1 text-right">{{ row.conflicts }}</td>
            </tr>
          </tbody>
        </table>

        <ul v-if="report.warnings.length" class="space-y-1 text-xs" data-test="mig-warnings">
          <li v-for="(w, i) in report.warnings" :key="i"
            class="border border-danger-border bg-danger px-2 py-1 text-danger-text">
            {{ w }}
          </li>
        </ul>

        <div v-if="report.conflicts.length" data-test="mig-report-conflicts"
          class="border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text">
          <p class="font-bold">{{ t('migration.plan.conflicts_title', { n: report.conflicts.length }) }}</p>
          <p class="text-xs">{{ t('migration.plan.conflicts_note') }}</p>
          <ul class="ml-4 mt-1 list-disc text-xs">
            <li v-for="(c, i) in report.conflicts.slice(0, conflictLimit)" :key="i">
              <span class="font-mono">{{ c.table }} {{ c.key }}</span>: {{ c.reason }}
            </li>
          </ul>
          <p v-if="report.conflicts.length > conflictLimit" class="mt-1 text-xs">
            {{ t('migration.plan.conflicts_more', { n: report.conflicts.length - conflictLimit }) }}
          </p>
        </div>

        <!-- Password reset (prominent) -->
        <div v-if="report.reset_required.length" data-test="mig-reset"
          class="space-y-2 border-2 border-danger-border bg-danger px-3 py-3 text-danger-text">
          <p class="text-sm font-bold">
            {{ t('migration.report.reset_title', { n: report.reset_required.length }) }}
          </p>
          <p class="text-xs">{{ t('migration.report.reset_text') }}</p>
          <p class="font-mono text-xs">{{ report.reset_required.join(', ') }}</p>
          <button v-if="!resetTokens.length" type="button" :disabled="busy" :class="buttonClass"
            data-test="mig-reset-generate" @click="generateResetTokens">
            {{ t('migration.report.reset_generate') }}
          </button>
          <div v-else data-test="mig-reset-tokens" class="border border-border bg-surface p-2 text-text">
            <div class="mb-1 flex items-center justify-between">
              <p class="text-xs font-bold">{{ t('migration.report.reset_once') }}</p>
              <button type="button" :class="secondaryButtonClass" data-test="mig-copy-tokens" @click="copyTokens">
                {{ tokensCopied ? t('migration.report.copied') : t('migration.report.copy_tokens') }}
              </button>
            </div>
            <table class="w-full font-mono text-xs">
              <tbody>
                <tr v-for="tok in resetTokens" :key="tok.username">
                  <td class="py-0.5 pr-4">{{ tok.username }}</td>
                  <td class="py-0.5">{{ tok.token }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- Operational order -->
        <div class="text-sm">
          <p class="mb-1 font-bold">{{ t('migration.report.order_title') }}</p>
          <!-- Steps arrive pre-numbered from the backend. -->
          <ol class="space-y-0.5 text-xs">
            <li v-for="(stepText, i) in report.operational_order" :key="i">{{ stepText }}</li>
          </ol>
        </div>

        <!-- Rsync suggestions -->
        <div v-if="report.rsync_suggestions.length" class="text-sm">
          <p class="mb-1 font-bold">{{ t('migration.report.rsync_title') }}</p>
          <p class="mb-1 text-xs text-text-muted">{{ t('migration.report.rsync_note') }}</p>
          <pre
            class="max-h-64 overflow-auto border border-border bg-bg p-2 text-xs"
            data-test="mig-rsync">{{ report.rsync_suggestions.join('\n') }}</pre>
        </div>
      </div>

      <div class="flex justify-end border-t border-border bg-bg px-4 py-3">
        <button type="button" :class="secondaryButtonClass" data-test="mig-new-migration" @click="startOver">
          {{ t('migration.start_over') }}
        </button>
      </div>
    </div>
  </div>
</template>

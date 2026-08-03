<script setup lang="ts">
// System state overview (port of show_sys_state.php): one card per server
// with the folded severity, OS/ISPConfig versions, the aggregated messages
// and links into the per-type detail views.
import { onMounted, onUnmounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import UiAlert from '../../components/UiAlert.vue'
import { checkTypes, formatTs, stateClass, type ServerState } from './state'

const { t } = useI18n()

const servers = ref<ServerState[]>([])
const error = ref('')
const loading = ref(false)
// Refresh intervals in minutes; 0 disables. Never below the 5-minute
// collection cadence — polling faster only re-reads the same rows.
const refreshOptions = [0, 5, 10, 15, 30, 60]
const refreshMinutes = ref(0)
let timer: number | undefined

async function load() {
  error.value = ''
  loading.value = true
  try {
    servers.value = (await api.get<ServerState[] | null>('/api/monitor/state')) ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    loading.value = false
  }
}

function applyRefresh() {
  if (timer !== undefined) window.clearInterval(timer)
  timer = undefined
  if (refreshMinutes.value > 0) {
    timer = window.setInterval(load, refreshMinutes.value * 60_000)
  }
}

onMounted(load)
onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer)
})
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('monitor.state_title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <div class="mb-3 flex items-center gap-2">
      <button type="button" data-test="monitor-refresh" class="btn btn-default px-4 py-2" @click="load">
        {{ t('monitor.refresh') }}
      </button>
      <label class="text-sm" for="monitor-auto-refresh">{{ t('monitor.auto_refresh') }}</label>
      <select
        id="monitor-auto-refresh"
        v-model.number="refreshMinutes"
        data-test="monitor-auto-refresh"
        class="border border-border bg-surface px-2 py-1 text-sm text-text"
        @change="applyRefresh"
      >
        <option v-for="m in refreshOptions" :key="m" :value="m">
          {{ m === 0 ? t('monitor.refresh_off') : t('monitor.refresh_minutes', { minutes: m }) }}
        </option>
      </select>
    </div>

    <p v-if="loading" data-test="monitor-loading" class="text-sm text-text-muted">
      {{ t('monitor.loading') }}
    </p>
    <p
      v-else-if="servers.length === 0"
      data-test="monitor-state-empty"
      class="border border-border bg-surface p-5 text-sm text-text-muted"
    >
      {{ t('monitor.no_data') }}
    </p>

    <ul v-else class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4">
      <li
        v-for="srv in servers"
        :key="srv.server_id"
        class="border border-border bg-surface"
        :data-test="`monitor-server-${srv.server_id}`"
      >
        <div class="flex items-center justify-between border-b border-border px-4 py-3">
          <span class="font-bold">{{ srv.server_name }}</span>
          <span
            class="border px-2 py-0.5 text-xs font-bold uppercase"
            :class="stateClass(srv.state)"
            data-test="monitor-server-state"
          >
            {{ t(`monitor.state.${srv.state}`) }}
          </span>
        </div>

        <dl class="px-4 py-3 text-sm">
          <div v-if="srv.os_name" class="flex justify-between">
            <dt class="text-text-muted">{{ t('monitor.os') }}</dt>
            <dd>{{ srv.os_name }} {{ srv.os_version }}</dd>
          </div>
          <div v-if="srv.ispc_version" class="flex justify-between">
            <dt class="text-text-muted">{{ t('monitor.ispc_version') }}</dt>
            <dd>{{ srv.ispc_version }}</dd>
          </div>
        </dl>

        <ul v-if="srv.messages?.length" class="border-t border-border px-4 py-3">
          <li
            v-for="(msg, i) in srv.messages"
            :key="i"
            class="mb-1 flex items-center gap-2 text-sm"
            data-test="monitor-message"
          >
            <span class="border px-1.5 text-xs uppercase" :class="stateClass(msg.severity)">
              {{ t(`monitor.state.${msg.severity}`) }}
            </span>
            <RouterLink :to="`/monitor/data/${msg.type}`" class="text-link">{{ msg.text }}</RouterLink>
          </li>
        </ul>

        <div class="border-t border-border bg-bg px-4 py-3">
          <p class="mb-2 text-xs font-bold uppercase text-text-muted">{{ t('monitor.checks') }}</p>
          <ul class="flex flex-wrap gap-1.5">
            <li v-for="typ in checkTypes" :key="typ">
              <RouterLink
                :to="`/monitor/data/${typ}?server_id=${srv.server_id}`"
                class="border border-border bg-surface px-2 py-0.5 text-xs no-underline"
                :title="formatTs(srv.checks?.find((c) => c.type === typ)?.created)"
              >
                {{ t(`monitor.type.${typ}`) }}
              </RouterLink>
            </li>
          </ul>
        </div>
      </li>
    </ul>
  </div>
</template>

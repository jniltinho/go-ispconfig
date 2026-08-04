<script setup lang="ts">
// Fail2ban jails (admin only): live state read from the daemon through
// /api/monitor/fail2ban/status, with a per-IP unban action. ISPConfig3 had
// no such page — its fail2ban surface was a raw log dump.
import { onMounted, ref } from 'vue'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import { useDialogStore } from '../../stores/dialog'

const { t } = useI18n()
const dialog = useDialogStore()

interface Jail {
  name: string
  currently_failed: number
  total_failed: number
  currently_banned: number
  total_banned: number
  banned_ips: string[] | null
}

const jails = ref<Jail[]>([])
const available = ref(true)
const error = ref('')
const loading = ref(false)

async function load() {
  error.value = ''
  loading.value = true
  try {
    const res = await api.get<{ available: boolean; jails: Jail[] | null }>(
      '/api/monitor/fail2ban/status',
    )
    available.value = res.available
    jails.value = res.jails ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    loading.value = false
  }
}

onMounted(load)

async function unban(jail: string, ip: string) {
  if (!(await dialog.confirm({ message: t('fail2ban.confirm_unban') }))) return
  error.value = ''
  try {
    await api.post(`/api/monitor/fail2ban/unban/${encodeURIComponent(jail)}/${encodeURIComponent(ip)}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  load()
}
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('fail2ban.title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="my-3" :messages="[t(error)]" />
    <UiAlert
      v-else-if="!loading && !available"
      variant="info"
      class="my-3"
      :messages="[t('fail2ban.unavailable')]"
    />

    <p v-if="loading" class="my-3 text-sm text-muted">{{ t('fail2ban.loading') }}</p>

    <div v-for="jail in jails" :key="jail.name" class="my-4 border border-border bg-surface p-4">
      <h2 class="text-lg font-semibold" data-test="fail2ban-jail">{{ jail.name }}</h2>
      <p class="text-sm text-muted">
        {{ t('fail2ban.currently_banned') }}: {{ jail.currently_banned }} ·
        {{ t('fail2ban.total_banned') }}: {{ jail.total_banned }} ·
        {{ t('fail2ban.currently_failed') }}: {{ jail.currently_failed }} ·
        {{ t('fail2ban.total_failed') }}: {{ jail.total_failed }}
      </p>

      <table v-if="jail.banned_ips?.length" class="mt-3 w-full text-sm">
        <thead>
          <tr class="text-left">
            <th class="py-1">{{ t('fail2ban.col.ip') }}</th>
            <th class="py-1">{{ t('fail2ban.col.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="ip in jail.banned_ips" :key="ip" class="border-t border-border">
            <td class="py-1">{{ ip }}</td>
            <td class="py-1">
              <button
                type="button"
                class="btn btn-danger px-3 py-1"
                data-test="fail2ban-unban"
                @click="unban(jail.name, ip)"
              >
                {{ t('fail2ban.unban') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="mt-3 text-sm text-muted">{{ t('fail2ban.no_bans') }}</p>
    </div>
  </div>
</template>

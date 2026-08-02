<script setup lang="ts">
// Delete confirmation for a client/reseller (client_del.php intent):
// shows the owned-resource counts, then offers a simple delete (keeps
// resources) or delete-everything (admin only, cascades resources and
// child clients).
import { onMounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useAuthStore } from '../../stores/auth'
import { useI18n } from '../../i18n'

const props = defineProps<{
  clientId: number
  username: string
  /** apiBase is /api/clients or /api/resellers (simple delete target). */
  apiBase: string
}>()
const emit = defineEmits<{ close: []; deleted: [] }>()

const { t } = useI18n()
const auth = useAuthStore()

const counts = ref<Record<string, number>>({})
const countsFailed = ref(false)
const error = ref('')
const busy = ref(false)

const countRows: { key: string; label: string }[] = [
  { key: 'web_domains', label: 'client.count_web_domains' },
  { key: 'web_folders', label: 'client.count_web_folders' },
  { key: 'dns_zones', label: 'client.count_dns_zones' },
  { key: 'dns_records', label: 'client.count_dns_records' },
  { key: 'dns_slaves', label: 'client.count_dns_slaves' },
  { key: 'child_clients', label: 'client.count_child_clients' },
]

onMounted(async () => {
  try {
    counts.value = await api.get<Record<string, number>>(
      `/api/clients/${props.clientId}/resource-counts`,
    )
  } catch {
    // Never present a failed lookup as "owns nothing": deletion stays
    // blocked until the counts are known.
    countsFailed.value = true
  }
})

async function doDelete(everything: boolean) {
  error.value = ''
  busy.value = true
  try {
    if (everything) {
      await api.delete(`/api/clients/${props.clientId}/everything`)
    } else {
      await api.delete(`${props.apiBase}/${props.clientId}`)
    }
    emit('deleted')
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40" data-test="delete-dialog">
    <div class="w-full max-w-md border border-border bg-surface p-4">
      <h2 class="mb-2 text-base font-bold">
        {{ t('client.delete_title', { username }) }}
      </h2>

      <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />
      <UiAlert
        v-if="countsFailed"
        variant="danger"
        class="mb-3"
        :messages="[t('client.counts_failed')]"
        data-test="counts-failed"
      />

      <p class="mb-2 text-sm">{{ t('client.delete_owned_intro') }}</p>
      <ul class="mb-3 text-sm">
        <li v-for="row in countRows" :key="row.key" class="flex justify-between border-b border-border py-0.5">
          <span>{{ t(row.label) }}</span>
          <span :data-test="`count-${row.key}`">{{ counts[row.key] ?? 0 }}</span>
        </li>
      </ul>
      <p class="mb-3 text-sm">{{ t('client.delete_choice_help') }}</p>

      <div class="flex flex-wrap justify-end gap-2">
        <button type="button" class="btn px-3 py-1.5" :disabled="busy" @click="emit('close')">
          {{ t('client.cancel') }}
        </button>
        <button
          type="button"
          class="border border-danger-border bg-danger px-3 py-1.5 text-danger-text"
          data-test="delete-simple"
          :disabled="busy || countsFailed"
          @click="doDelete(false)"
        >
          {{ t('client.delete_simple') }}
        </button>
        <button
          v-if="auth.typ === 'admin'"
          type="button"
          class="border border-danger-border bg-danger px-3 py-1.5 font-bold text-danger-text"
          data-test="delete-everything"
          :disabled="busy || countsFailed"
          @click="doDelete(true)"
        >
          {{ t('client.delete_everything') }}
        </button>
      </div>
    </div>
  </div>
</template>

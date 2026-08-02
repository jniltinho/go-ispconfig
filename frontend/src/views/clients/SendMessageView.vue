<script setup lang="ts">
// Send an email to clients (port of client_message.php): one client or
// every client in scope, subject/body typed or loaded from a template.
import { onMounted, ref } from 'vue'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const { t } = useI18n()

type Opt = { value: string; label: string }
const clients = ref<Opt[]>([])
const templates = ref<{ id: number; name: string; subject: string; message: string }[]>([])

const recipient = ref('') // '' = all in scope, else client_id
const templateSel = ref('')
const subject = ref('')
const message = ref('')
const sending = ref(false)
const error = ref('')
const success = ref('')

interface ListResponse {
  items: Record<string, unknown>[]
}

onMounted(async () => {
  try {
    const res = await api.get<ListResponse>('/api/clients?limit=100')
    clients.value = res.items.map((c) => ({
      value: String(c.client_id),
      label: `${c.contact_name ?? ''} (${c.username})`,
    }))
  } catch {
    clients.value = []
  }
  try {
    const res = await api.get<ListResponse>('/api/client-message-templates?limit=100')
    templates.value = res.items.map((tp) => ({
      id: Number(tp.client_message_template_id),
      name: String(tp.template_name),
      subject: String(tp.subject ?? ''),
      message: String(tp.message ?? ''),
    }))
  } catch {
    templates.value = []
  }
})

function loadTemplate() {
  const tp = templates.value.find((x) => String(x.id) === templateSel.value)
  if (!tp) return
  subject.value = tp.subject
  message.value = tp.message
}

async function send() {
  error.value = ''
  success.value = ''
  sending.value = true
  try {
    const body: Record<string, unknown> = { subject: subject.value, message: message.value }
    if (recipient.value) body.client_ids = [Number(recipient.value)]
    const res = await api.post<{ sent: number; skipped: number }>('/api/clients/send-message', body)
    success.value = t('client.message_sent', { sent: String(res.sent), skipped: String(res.skipped) })
  } catch (e) {
    if (e instanceof ApiError && e.fields?.smtp) {
      error.value = t('client.smtp_disabled')
    } else if (e instanceof ApiError && e.status === 422) {
      error.value = t('error.validation_failed')
    } else {
      error.value = t(e instanceof ApiError ? e.key : 'error.request_failed')
    }
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <div class="max-w-2xl">
    <h1 class="mb-3 text-lg font-bold">{{ t('client.send_message_title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[error]" data-test="send-error" />
    <UiAlert v-if="success" variant="info" class="mb-3" :messages="[success]" data-test="send-success" />

    <form class="space-y-3" @submit.prevent="send">
      <div>
        <label class="mb-1 block text-sm font-bold" for="recipient">{{ t('client.recipient') }}</label>
        <select id="recipient" v-model="recipient" class="w-full border border-border bg-surface px-3 py-1.5 text-sm">
          <option value="">{{ t('client.all_clients') }}</option>
          <option v-for="c in clients" :key="c.value" :value="c.value">{{ c.label }}</option>
        </select>
      </div>

      <div v-if="templates.length">
        <label class="mb-1 block text-sm font-bold" for="msg-template">{{ t('client.load_template') }}</label>
        <div class="flex gap-2">
          <select id="msg-template" v-model="templateSel" class="border border-border bg-surface px-3 py-1.5 text-sm">
            <option value="">{{ t('client.pick_template') }}</option>
            <option v-for="tp in templates" :key="tp.id" :value="String(tp.id)">{{ tp.name }}</option>
          </select>
          <button type="button" class="btn px-3 py-1" data-test="load-template" @click="loadTemplate">
            {{ t('client.load') }}
          </button>
        </div>
      </div>

      <div>
        <label class="mb-1 block text-sm font-bold" for="msg-subject">{{ t('subject_txt') }}</label>
        <input
          id="msg-subject"
          v-model="subject"
          type="text"
          class="w-full border border-border bg-surface px-3 py-1.5 text-sm"
        />
      </div>

      <div>
        <label class="mb-1 block text-sm font-bold" for="msg-body">{{ t('message_txt') }}</label>
        <textarea
          id="msg-body"
          v-model="message"
          rows="10"
          class="w-full border border-border bg-surface px-3 py-1.5 text-sm"
        />
        <p class="mt-1 text-xs text-text/70">{{ t('client.placeholder_help') }}</p>
      </div>

      <button type="submit" class="btn btn-success px-4 py-2" :disabled="sending" data-test="send-message">
        {{ t('client.send') }}
      </button>
    </form>
  </div>
</template>

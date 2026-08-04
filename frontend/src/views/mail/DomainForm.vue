<script setup lang="ts">
// Mail domain form: the metadata-driven EntityForm plus the Spamfilter
// policy options and the DKIM controls (generate key, suggested DNS TXT,
// auto-publish indicator). The DKIM panel renders inside the form, above
// the Save/Cancel bar, next to the collapsed DKIM fieldset. The generate
// button fills the private/public fields; on save the API reconciles DNS
// and returns dns_published.
import { onMounted, ref } from 'vue'
import EntityForm from '../sites/EntityForm.vue'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const props = defineProps<{ id?: string }>()
const { t } = useI18n()

type Opt = { value: string; label: string }

const suggested = ref('')
const dnsPublished = ref<boolean | null>(null)
const error = ref('')
const overrides = ref<Record<string, Opt[]>>({})
const ready = ref(false)

onMounted(async () => {
  try {
    const policies = await api.get<Opt[]>('/api/meta/lookups/spamfilter-policies')
    // Leading "- not enabled -" entry matches the entity default 0
    // (legacy <option value='0'>{no_policy}</option>).
    overrides.value = {
      policy: [
        { value: '0', label: t('mail.no_policy') },
        ...(policies ?? []).map((p) => ({ value: String(p.value), label: String(p.label) })),
      ],
    }
  } catch {
    // Policy select falls back to a free-text id when unavailable.
  }
  if (props.id) {
    try {
      const rec = await api.get<Record<string, unknown>>(`/api/mail/domains/${props.id}`)
      suggested.value = String(rec.suggested_record ?? '')
      if (typeof rec.dns_published === 'boolean') dnsPublished.value = rec.dns_published
    } catch {
      // Non-fatal: the form still loads via EntityForm.
    }
  }
  ready.value = true
})

// generateKey asks the API for a fresh pair and injects it into the form
// fields (dkim_private/dkim_public are textareas rendered by EntityForm).
async function generateKey() {
  error.value = ''
  try {
    const pair = await api.post<{ dkim_private: string; dkim_public: string }>(
      '/api/mail/domains/generate-dkim',
    )
    setField('dkim_private', pair.dkim_private)
    setField('dkim_public', pair.dkim_public)
    setField('dkim', true)
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

// setField writes into an EntityForm control via a real input event so
// its v-model updates (the form has no exposed API; this is the same
// technique the E2E harness uses).
function setField(name: string, value: string | boolean) {
  const el = document.querySelector<HTMLInputElement>(`#field-${name}`)
  if (!el) return
  if (typeof value === 'boolean') {
    if (el.checked !== value) {
      el.checked = value
      el.dispatchEvent(new Event('change', { bubbles: true }))
    }
    return
  }
  el.value = value
  el.dispatchEvent(new Event('input', { bubbles: true }))
}
</script>

<template>
  <EntityForm
    v-if="ready"
    entity="domains"
    api-base="/api/mail/domains"
    back-to="/mail"
    :id="id"
    :option-overrides="overrides"
  >
    <template #extra>
      <section class="border-t border-border px-4 py-3" data-test="dkim-panel">
        <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />
        <button type="button" class="btn px-3 py-1.5" data-test="dkim-generate" @click="generateKey">
          {{ t('mail.dkim_generate') }}
        </button>

        <div v-if="suggested" class="mt-3">
          <label class="mb-1 block text-sm font-bold">{{ t('mail.dkim_suggested_txt') }}</label>
          <pre
            class="overflow-x-auto whitespace-pre-wrap break-all border border-border bg-bg p-2 text-xs"
            data-test="dkim-suggested"
          >{{ suggested }}</pre>
        </div>

        <p
          v-if="dnsPublished === true"
          class="mt-3 border border-border bg-info px-3 py-2 text-sm"
          data-test="dkim-auto-dns"
        >
          {{ t('mail.dkim_auto_dns') }}
        </p>
        <p
          v-else-if="dnsPublished === false && suggested"
          class="mt-3 text-sm text-text/70"
          data-test="dkim-manual-dns"
        >
          {{ t('mail.dkim_manual_dns') }}
        </p>
      </section>
    </template>
  </EntityForm>
</template>

<script setup lang="ts">
// Zone wizard (port of dns_wizard.php): pick a visible template, fill only
// the inputs its fields CSV declares, create the zone via the wizard
// endpoint and open the new zone's form on the Records tab.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const router = useRouter()

interface TemplateInfo {
  template_id: number
  name: string
  fields: string
}

const templates = ref<TemplateInfo[]>([])
const templateId = ref<number | null>(null)
const values = ref({ domain: '', ip: '', ipv6: '', ns1: '', ns2: '', email: '' })
const dnssec = ref(false)
const errors = ref<Record<string, string[]>>({})
const error = ref('')
const saving = ref(false)

const currentFields = computed(() => {
  const tpl = templates.value.find((tp) => tp.template_id === templateId.value)
  return new Set((tpl?.fields ?? '').split(',').map((f) => f.trim()))
})

// Text inputs of the wizard, shown only when the template declares them.
const textInputs = [
  { field: 'DOMAIN', key: 'domain' as const, label: 'dns.wizard.domain' },
  { field: 'IP', key: 'ip' as const, label: 'dns.wizard.ip' },
  { field: 'IPV6', key: 'ipv6' as const, label: 'dns.wizard.ipv6' },
  { field: 'NS1', key: 'ns1' as const, label: 'dns.wizard.ns1' },
  { field: 'NS2', key: 'ns2' as const, label: 'dns.wizard.ns2' },
  { field: 'EMAIL', key: 'email' as const, label: 'dns.wizard.email' },
]

onMounted(async () => {
  try {
    templates.value = (await api.get<TemplateInfo[]>('/api/dns/templates')) ?? []
    templateId.value = templates.value[0]?.template_id ?? null
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

async function create() {
  if (templateId.value === null) return
  errors.value = {}
  error.value = ''
  saving.value = true
  try {
    const zone = await api.post<Record<string, unknown>>('/api/dns/zones/wizard', {
      template_id: templateId.value,
      ...values.value,
      dnssec: dnssec.value,
    })
    router.push(`/dns/zones/${zone.id}`)
  } catch (e) {
    if (e instanceof ApiError && e.status === 422 && e.fields) {
      const translated: Record<string, string[]> = {}
      for (const [field, keys] of Object.entries(e.fields)) {
        translated[field] = keys.map((key) => t(key))
      }
      errors.value = translated
    } else {
      error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <h1 class="mb-3 text-lg font-bold">{{ t('dns.wizard_title') }}</h1>

    <p
      v-if="error"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
    >
      {{ t(error) }}
    </p>

    <form class="max-w-2xl border border-border bg-surface" @submit.prevent="create">
      <div class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="wizard-template" class="w-48 shrink-0 pt-1.5 text-right text-sm font-medium after:content-[':']">
            {{ t('dns.wizard.template') }}
          </label>
          <select
            id="wizard-template"
            v-model="templateId"
            data-test="wizard-template"
            class="w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none"
          >
            <option v-for="tpl in templates" :key="tpl.template_id" :value="tpl.template_id">
              {{ tpl.name }}
            </option>
          </select>
        </div>

        <div v-for="input in textInputs" :key="input.key">
          <div v-if="currentFields.has(input.field)" class="flex items-start gap-4">
            <label
              :for="`wizard-${input.key}`"
              class="w-48 shrink-0 pt-1.5 text-right text-sm font-medium after:content-[':']"
            >
              {{ t(input.label) }}
            </label>
            <div class="flex-1">
              <input
                :id="`wizard-${input.key}`"
                v-model="values[input.key]"
                type="text"
                class="w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link"
                :class="{ 'border-danger-border': errors[input.key]?.length }"
              />
              <p v-if="errors[input.key]?.length" class="mt-1 text-xs text-danger-text">
                {{ errors[input.key].join(', ') }}
              </p>
            </div>
          </div>
        </div>

        <div v-if="currentFields.has('DNSSEC')" class="flex items-center gap-4">
          <span class="w-48 shrink-0 text-right text-sm font-medium after:content-[':']">
            {{ t('dns.wizard.dnssec') }}
          </span>
          <input id="wizard-dnssec" v-model="dnssec" type="checkbox" />
        </div>
      </div>

      <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
        <button
          type="button"
          class="border border-border bg-surface px-8 py-1.5 text-xs font-bold hover:bg-info"
          @click="router.push('/dns')"
        >
          {{ t('form.cancel') }}
        </button>
        <button
          type="submit"
          data-test="wizard-create"
          :disabled="saving || templateId === null"
          class="bg-success px-8 py-1.5 text-xs font-bold text-white hover:opacity-90 disabled:opacity-50"
        >
          {{ t('dns.wizard.create') }}
        </button>
      </div>
    </form>
  </div>
</template>

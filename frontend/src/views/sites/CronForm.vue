<script setup lang="ts">
// Cron job form: the metadata-driven EntityForm plus the parent website
// select (vhost datasource parity), the placeholder help of the legacy
// panel and client-side schedule/command checks.
import { onMounted, ref } from 'vue'
import EntityForm from './EntityForm.vue'
import { api } from '../../api'
import { useI18n } from '../../i18n'

const props = defineProps<{
  /** id is the cron id; absent for the create form. */
  id?: string
}>()

const { t } = useI18n()

type Opt = { value: string; label: string }
const overrides = ref<Record<string, Opt[]>>({})
const ready = ref(false)

interface ListResponse {
  items: Record<string, unknown>[]
}

onMounted(async () => {
  try {
    const domains = await api.get<ListResponse>('/api/sites/web-domains?type=vhost&limit=100')
    overrides.value = {
      parent_domain_id: domains.items.map((d) => ({
        value: String(d.domain_id),
        label: String(d.domain),
      })),
    }
  } catch {
    // The field stays a text input when the website list cannot be loaded.
  }
  ready.value = true
})

// Schedule charset of validate_cron.inc.php run_time_format / run_month_format:
// digits, '*', ',', '-' and '/' — plus '@reboot' for the month field. The API
// re-validates ranges; this only catches obvious typos before the round trip.
const scheduleRE = /^[0-9*,\-/]+$/
const scheduleFields = ['run_min', 'run_hour', 'run_mday', 'run_month', 'run_wday']

function validate(values: Record<string, unknown>): Record<string, string[]> {
  const errors: Record<string, string[]> = {}
  for (const field of scheduleFields) {
    const value = String(values[field] ?? '').replace(/\s/g, '')
    if (value === '') {
      errors[field] = [t(`${field}_error_empty`)]
    } else if (!(field === 'run_month' && value === '@reboot') && !scheduleRE.test(value)) {
      errors[field] = [t(`${field}_error_format`)]
    }
  }
  const command = String(values.command ?? '').trim()
  if (command === '') errors.command = [t('command_error_empty')]
  if (!values.parent_domain_id) {
    errors.parent_domain_id = [t('parent_domain_id_error_empty')]
  }
  return errors
}
</script>

<template>
  <div>
    <p class="mb-3 border border-border bg-info px-3 py-2 text-xs">{{ t('command_hint_txt') }}</p>

    <EntityForm
      v-if="ready"
      entity="crons"
      api-base="/api/sites/crons"
      back-to="/sites/crons"
      :id="props.id"
      :option-overrides="overrides"
      :validate="validate"
      :readonly-fields="props.id ? ['parent_domain_id', 'server_id', 'type'] : ['server_id', 'type']"
    />
  </div>
</template>

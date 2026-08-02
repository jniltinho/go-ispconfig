<script setup lang="ts">
// Client / reseller form: the metadata-driven EntityForm plus the
// API-fed selects (country, parent reseller, master template) and the
// additional-template assignment manager (edit mode).
import { onMounted, ref } from 'vue'
import EntityForm from '../sites/EntityForm.vue'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useAuthStore } from '../../stores/auth'
import { useI18n } from '../../i18n'

const props = withDefaults(
  defineProps<{
    /** id is the client_id; absent for the create form. */
    id?: string
    /** reseller switches to the /api/resellers surface. */
    reseller?: boolean
  }>(),
  { reseller: false },
)

const { t } = useI18n()
const auth = useAuthStore()
const apiBase = props.reseller ? '/api/resellers' : '/api/clients'
const entity = props.reseller ? 'resellers' : 'clients'
const backTo = props.reseller ? '/clients/resellers' : '/clients'

type Opt = { value: string; label: string }
const overrides = ref<Record<string, Opt[]>>({})
const ready = ref(false)
const error = ref('')

interface ListResponse {
  items: Record<string, unknown>[]
  total: number
}

async function listAll(path: string): Promise<Record<string, unknown>[]> {
  const res = await api.get<ListResponse>(`${path}?limit=100`)
  return res.items
}

onMounted(async () => {
  const o: Record<string, Opt[]> = {}
  try {
    const countries = await api.get<{ iso: string; printable_name: string }[]>('/api/countries')
    o.country = [
      { value: '', label: '' },
      ...countries.map((c) => ({ value: c.iso, label: c.printable_name })),
    ]
  } catch {
    // Countries stay a free text field when the list cannot be loaded.
  }
  if (auth.typ === 'admin') {
    try {
      if (!props.reseller) {
        const resellers = await listAll('/api/resellers')
        o.parent_client_id = [
          { value: '0', label: t('client.no_parent') },
          ...resellers.map((r) => ({
            value: String(r.client_id),
            label: `${r.contact_name ?? ''} (${r.username})`,
          })),
        ]
      }
      const templates = await listAll('/api/client-templates')
      o.template_master = [
        { value: '0', label: t('client.template_custom') },
        ...templates
          .filter((tp) => tp.template_type === 'm')
          .map((tp) => ({ value: String(tp.template_id), label: String(tp.template_name) })),
      ]
      additionalOptions.value = templates
        .filter((tp) => tp.template_type === 'a')
        .map((tp) => ({ value: String(tp.template_id), label: String(tp.template_name) }))
    } catch {
      // Non-fatal: template/parent selects fall back to plain inputs.
    }
  }
  overrides.value = o
  if (props.id) await loadAssigned()
  ready.value = true
})

// --- additional template assignments (edit mode, task 4.3 endpoints) ---

interface Assigned {
  assigned_template_id: number
  client_template_id: number
  template_name: string
}
const assigned = ref<Assigned[]>([])
const additionalOptions = ref<Opt[]>([])
const addSelection = ref('')

async function loadAssigned() {
  try {
    assigned.value = await api.get<Assigned[]>(`/api/clients/${props.id}/templates`)
  } catch {
    assigned.value = []
  }
}

async function addTemplate() {
  if (!addSelection.value || !props.id) return
  error.value = ''
  try {
    await api.post(`/api/clients/${props.id}/templates`, {
      template_id: Number(addSelection.value),
    })
    await loadAssigned()
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

async function removeTemplate(row: Assigned) {
  if (!props.id) return
  error.value = ''
  try {
    await api.delete(`/api/clients/${props.id}/templates/${row.assigned_template_id}`)
    await loadAssigned()
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}
</script>

<template>
  <div>
    <EntityForm
      v-if="ready"
      :entity="entity"
      :api-base="apiBase"
      :back-to="backTo"
      :id="id"
      :option-overrides="overrides"
    />

    <section v-if="ready && id" class="mt-6 border border-border bg-surface p-4" data-test="additional-templates">
      <h2 class="mb-2 text-base font-bold">{{ t('client.additional_templates') }}</h2>
      <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />
      <ul v-if="assigned.length" class="mb-3">
        <li
          v-for="row in assigned"
          :key="row.assigned_template_id"
          class="flex items-center justify-between border-b border-border py-1"
        >
          <span>{{ row.template_name || row.client_template_id }}</span>
          <button
            type="button"
            class="border border-danger-border bg-danger px-2 py-0.5 text-xs text-danger-text"
            data-test="remove-template"
            @click="removeTemplate(row)"
          >
            {{ t('client.remove_template') }}
          </button>
        </li>
      </ul>
      <p v-else class="mb-3 text-sm">{{ t('client.no_additional_templates') }}</p>
      <div v-if="additionalOptions.length" class="flex items-center gap-2">
        <select v-model="addSelection" class="border border-border bg-surface px-2 py-1" data-test="add-template-select">
          <option value="">{{ t('client.pick_template') }}</option>
          <option v-for="o in additionalOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
        <button type="button" class="btn btn-success px-3 py-1" data-test="add-template" @click="addTemplate">
          {{ t('client.assign_template') }}
        </button>
      </div>
    </section>
  </div>
</template>

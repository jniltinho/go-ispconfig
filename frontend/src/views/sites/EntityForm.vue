<script setup lang="ts">
// Generic metadata-driven entity form: tabs and fields come from
// GET /api/meta/forms/{entity} (the same source of truth the API validates
// against), values are loaded from / saved to the entity endpoint, and 422
// responses are mapped back onto the fields (TabbedForm stays on the
// offending tab). No hardcoded per-tab components.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import TabbedForm, { type FormMetadata } from '../../components/TabbedForm.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const props = defineProps<{
  /** entity is the form metadata name (e.g. "web-domains"). */
  entity: string
  /** apiBase is the CRUD endpoint (e.g. "/api/sites/web-domains"). */
  apiBase: string
  /** backTo is the list route to return to on save/cancel. */
  backTo: string
  /** id is the record primary key; absent for the create form. */
  id?: string
  /** fixed values are merged into every save (e.g. web_folder_id). */
  fixed?: Record<string, unknown>
}>()

const { t } = useI18n()
const router = useRouter()

/** Server form metadata (field shape of GET /api/meta/forms/{entity}). */
interface ServerField {
  name: string
  label: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox'
  datatype: string
  formtype: string
  default?: unknown
  options?: { value: string; label: string }[]
}
interface ServerMeta {
  name: string
  title: string
  tabs: { name: string; label: string; fields: ServerField[] }[]
}

const serverMeta = ref<ServerMeta | null>(null)
const metadata = ref<FormMetadata | null>(null)
const initial = ref<Record<string, unknown>>({})
const errors = ref<Record<string, string[]>>({})
const title = ref('')
const loadError = ref('')
const datalogState = ref('')
const datalogError = ref('')
const saving = ref(false)

/** truthyOption returns the checkbox value meaning "checked" ('y', '1', …). */
function truthyOption(field: ServerField): string {
  return field.options?.at(-1)?.value ?? 'y'
}

/** falsyOption returns the checkbox value meaning "unchecked" ('n', '0', …). */
function falsyOption(field: ServerField): string {
  return field.options?.[0]?.value ?? 'n'
}

// toFormMetadata translates labels/options through i18n and converts
// defaults to the client control types (checkbox booleans, string values).
function toFormMetadata(meta: ServerMeta): FormMetadata {
  return {
    tabs: meta.tabs.map((tab) => ({
      name: tab.name,
      label: t(tab.label),
      fields: tab.fields.map((field) => ({
        name: field.name,
        // SELECTs whose options come from a server datasource (not ported
        // yet, e.g. server_id) arrive without options; render them as text
        // inputs so the value stays editable.
        type: field.type === 'select' && !field.options?.length ? 'text' : field.type,
        label: t(field.label),
        options: field.options?.map((o) => ({ value: o.value, label: t(o.label) })),
        default:
          field.type === 'checkbox'
            ? String(field.default ?? '') === truthyOption(field)
            : field.default == null
              ? undefined
              : String(field.default),
      })),
    })),
  }
}

// toFormValues converts an API record into TabbedForm values: checkboxes
// become booleans, everything else a string (selects/number inputs).
function toFormValues(meta: ServerMeta, record: Record<string, unknown>): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  for (const tab of meta.tabs) {
    for (const field of tab.fields) {
      const v = record[field.name]
      if (v == null) continue
      values[field.name] = field.type === 'checkbox' ? String(v) === truthyOption(field) : String(v)
    }
  }
  return values
}

// toPayload converts submitted TabbedForm values back to the API shape.
// Empty password fields are omitted (keep the stored hash on update).
function toPayload(meta: ServerMeta, values: Record<string, unknown>): Record<string, unknown> {
  const payload: Record<string, unknown> = {}
  for (const tab of meta.tabs) {
    for (const field of tab.fields) {
      const v = values[field.name]
      if (v === undefined) continue
      if (field.type === 'checkbox') {
        payload[field.name] = v ? truthyOption(field) : falsyOption(field)
      } else if (v === '' && (field.type === 'password' || field.datatype === 'INTEGER')) {
        // Untouched password (keep the stored hash) or empty numeric input
        // (let the server default apply instead of failing conversion).
        continue
      } else {
        payload[field.name] = v
      }
    }
  }
  return { ...payload, ...props.fixed }
}

onMounted(async () => {
  try {
    const meta = await api.get<ServerMeta>(`/api/meta/forms/${props.entity}`)
    let record: Record<string, unknown> = {}
    if (props.id) {
      record = await api.get<Record<string, unknown>>(`${props.apiBase}/${props.id}`)
      datalogState.value = String(record._datalog_state ?? '')
      datalogError.value = String(record._datalog_error ?? '')
    }
    serverMeta.value = meta
    title.value = t(meta.title)
    initial.value = toFormValues(meta, { ...record, ...(props.id ? {} : (props.fixed ?? {})) })
    metadata.value = toFormMetadata(meta)
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

async function save(values: Record<string, unknown>) {
  if (!serverMeta.value) return
  errors.value = {}
  saving.value = true
  try {
    const payload = toPayload(serverMeta.value, values)
    if (props.id) {
      await api.put(`${props.apiBase}/${props.id}`, payload)
    } else {
      await api.post(props.apiBase, payload)
    }
    router.push(props.backTo)
  } catch (e) {
    if (e instanceof ApiError && e.status === 422 && e.fields) {
      // Translate the per-field i18n keys for inline display.
      const translated: Record<string, string[]> = {}
      for (const [field, keys] of Object.entries(e.fields)) {
        translated[field] = keys.map((key) => t(key))
      }
      errors.value = translated
    } else {
      loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <h1 class="mb-3 text-lg font-bold">{{ title }}</h1>

    <p
      v-if="datalogState === 'pending'"
      class="mb-3 border border-border bg-info px-3 py-2 text-sm"
      data-test="state-pending"
    >
      {{ t('sites.state.pending') }}
    </p>
    <p
      v-else-if="datalogState === 'error'"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
      data-test="state-error"
    >
      {{ t('sites.state.error') }}: {{ datalogError }}
    </p>

    <p
      v-if="loadError"
      class="mb-3 border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
    >
      {{ t(loadError) }}
    </p>

    <TabbedForm
      v-if="metadata"
      :metadata="metadata"
      :model-value="initial"
      :errors="errors"
      @save="save"
      @cancel="router.push(backTo)"
    />
  </div>
</template>

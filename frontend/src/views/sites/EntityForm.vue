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
import UiAlert from '../../components/UiAlert.vue'
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
  /** embedded hides the h1 title (form rendered inside another view). */
  embedded?: boolean
  /** readonlyFields render disabled (e.g. the server-managed serial). */
  readonlyFields?: string[]
  /**
   * optionOverrides force a field to render as a select with the given
   * options (e.g. country list or parent reseller ids fetched from the
   * API by the wrapping view).
   */
  optionOverrides?: Record<string, { value: string; label: string }[]>
  /**
   * resolveSelectOptions supplies select options that depend on the current
   * form values (e.g. server IPs filtered by server_id). Runs after static
   * optionOverrides.
   */
  resolveSelectOptions?: (
    fieldName: string,
    values: Record<string, unknown>,
  ) => { value: string; label: string }[] | undefined
  /**
   * hideFields returns field names to omit from the rendered form for the
   * current values (legacy template conditionals the metadata API cannot
   * express per keystroke, e.g. PHP Version only when PHP is php-fpm).
   */
  hideFields?: (values: Record<string, unknown>) => string[]
  /**
   * metadataDeps lists the value keys that drive resolveSelectOptions (e.g.
   * ['server_id', 'type']). Changes to other keys skip the metadata rebuild;
   * absent with a resolveSelectOptions prop, every change rebuilds.
   */
  metadataDeps?: string[]
  /**
   * validate runs client-side before the save call and, when it returns
   * field errors, blocks the request (mirrors the API rules for instant
   * feedback; the API stays the authority).
   */
  validate?: (values: Record<string, unknown>) => Record<string, string[]>
}>()

const { t } = useI18n()
const router = useRouter()

/** Server form metadata (field shape of GET /api/meta/forms/{entity}). */
interface ServerField {
  name: string
  label: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox' | 'checkboxarray' | 'legend'
  datatype: string
  formtype: string
  collapsible?: boolean
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
/** Merged option overrides: parent props + auto-loaded server_id lookup. */
const resolvedOverrides = ref<Record<string, { value: string; label: string }[]>>({})
/** Live form values for dynamic select resolution. */
const liveValues = ref<Record<string, unknown>>({})
/** Last metadata-driving signature; skips rebuilds on unrelated keystrokes. */
let lastMetadataSig = ''

function metadataSignature(values: Record<string, unknown>): string {
  const parts: string[] = []
  if (props.hideFields) {
    parts.push(props.hideFields(values).slice().sort().join(','))
  }
  if (props.resolveSelectOptions) {
    if (!props.metadataDeps) return `${parts.join('|')}|${JSON.stringify(values)}`
    for (const key of props.metadataDeps) {
      parts.push(`${key}=${String(values[key] ?? '')}`)
    }
  }
  return parts.join('|')
}

function refreshMetadata() {
  if (!serverMeta.value) return
  metadata.value = toFormMetadata(serverMeta.value, liveValues.value)
}

function onValuesChange(values: Record<string, unknown>) {
  liveValues.value = values
  if (!props.resolveSelectOptions && !props.hideFields) return
  const sig = metadataSignature(values)
  if (sig === lastMetadataSig) return
  lastMetadataSig = sig
  refreshMetadata()
}

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
function toFormMetadata(meta: ServerMeta, values: Record<string, unknown> = {}): FormMetadata {
  const hidden = new Set(props.hideFields?.(values) ?? [])
  return {
    tabs: meta.tabs.map((tab) => ({
      name: tab.name,
      label: t(tab.label),
      // Skip server-only columns (empty label) e.g. dual-hash password_sha2.
      fields: tab.fields
        .filter((field) => field.label !== '' && !hidden.has(field.name))
        .map((field) => {
        const dynamic = props.resolveSelectOptions?.(field.name, values)
        const override = dynamic ?? resolvedOverrides.value[field.name]
        return {
        name: field.name,
        // SELECTs whose options come from a server datasource (not ported
        // yet, e.g. server_id) arrive without options; render them as text
        // inputs so the value stays editable — unless resolvedOverrides
        // filled them (auto server lookup or parent optionOverrides).
        type: override
          ? ('select' as const)
          : field.type === 'select' && !field.options?.length
            ? 'text'
            : field.type,
        readonly: props.readonlyFields?.includes(field.name) || undefined,
        // LEGEND fields carry the collapse flag; TabbedForm folds the
        // section that follows into a <details> (legacy #toggle-dkim).
        collapsible: field.collapsible,
        // Quota fields use the legacy MB addon (web_vhost_domain_edit.htm).
        suffix: field.name === 'hd_quota' || field.name === 'traffic_quota' ? 'MB' : undefined,
        label: t(field.label),
        options: override ?? field.options?.map((o) => ({ value: o.value, label: t(o.label) })),
        default:
          field.type === 'checkbox'
            ? String(field.default ?? '') === truthyOption(field)
            : field.default == null
              ? undefined
              : String(field.default),
        }
      }),
    })),
  }
}

/**
 * autoLookups maps the SELECT fields shared across entities to their
 * datasource endpoint, so every form gets a real dropdown instead of the
 * free-text fallback. `empty` is the leading "not assigned" option matching
 * the entity default (legacy <option value='0'>).
 */
const autoLookups: Record<string, { url: string; empty?: { value: string; label: string } }> = {
  server_id: { url: '/api/meta/lookups/servers' },
  client_group_id: { url: '/api/meta/lookups/client-groups', empty: { value: '0', label: '—' } },
}

/** needsLookup is true when a bare SELECT has neither options nor an override. */
function needsLookup(meta: ServerMeta, name: string): boolean {
  if (resolvedOverrides.value[name]?.length) return false
  return meta.tabs.some((tab) =>
    tab.fields.some(
      (field) => field.name === name && field.type === 'select' && !field.options?.length,
    ),
  )
}

// toFormValues converts an API record into TabbedForm values: checkboxes
// become booleans, everything else a string (selects/number inputs).
function toFormValues(meta: ServerMeta, record: Record<string, unknown>): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  for (const tab of meta.tabs) {
    for (const field of tab.fields) {
      // Stored password hashes never reach the form: the field starts
      // empty and an empty value is omitted from the save payload.
      if (field.type === 'password') continue
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
    resolvedOverrides.value = { ...(props.optionOverrides ?? {}) }
    const meta = await api.get<ServerMeta>(`/api/meta/forms/${props.entity}`)
    // Auto-fill the shared selects (server_id, client_group_id) so mail/dns/
    // firewall/sites forms do not fall back to free-text ids.
    for (const [name, src] of Object.entries(autoLookups)) {
      if (!needsLookup(meta, name)) continue
      try {
        const rows = await api.get<{ value: string; label: string }[]>(src.url)
        const opts = (rows ?? []).map((r) => ({
          value: String(r.value),
          label: String(r.label),
        }))
        // Always apply when empty is declared — a zero-length client list
        // must still show the legacy leading "—" (value 0), not a bare "0"
        // text input.
        if (!opts.length && !src.empty) continue
        resolvedOverrides.value = {
          ...resolvedOverrides.value,
          [name]: src.empty ? [src.empty, ...opts] : opts,
        }
      } catch {
        // Lookup failed: still offer the empty placeholder when one exists
        // (client-groups with no clients / transient 5xx) so the field stays
        // a select, matching legacy.
        if (src.empty) {
          resolvedOverrides.value = {
            ...resolvedOverrides.value,
            [name]: [src.empty],
          }
        }
      }
    }
    let record: Record<string, unknown> = {}
    if (props.id) {
      record = await api.get<Record<string, unknown>>(`${props.apiBase}/${props.id}`)
      datalogState.value = String(record._datalog_state ?? '')
      datalogError.value = String(record._datalog_error ?? '')
    }
    serverMeta.value = meta
    title.value = t(meta.title)
    initial.value = toFormValues(meta, { ...record, ...(props.id ? {} : (props.fixed ?? {})) })
    // A <select> shows its first option when nothing is selected; mirror that
    // in the values so fields that depend on it (server IPs on the create
    // form) resolve against the server the user actually sees.
    for (const [name, opts] of Object.entries(resolvedOverrides.value)) {
      if (initial.value[name] === undefined && opts.length) initial.value[name] = opts[0].value
    }
    // Seed metadata defaults into the create payload so hideFields /
    // resolveSelectOptions see the same values TabbedForm will render
    // (otherwise php stays unset and PHP Version stays hidden on first paint).
    // Skip empty-label server-only columns (same filter as toFormMetadata).
    if (!props.id) {
      for (const tab of meta.tabs) {
        for (const field of tab.fields) {
          if (field.label === '' || initial.value[field.name] !== undefined) continue
          if (field.type === 'checkbox') {
            initial.value[field.name] =
              String(field.default ?? '') === truthyOption(field)
          } else if (field.default != null) {
            initial.value[field.name] = String(field.default)
          }
        }
      }
    }
    liveValues.value = { ...initial.value }
    lastMetadataSig = metadataSignature(liveValues.value)
    metadata.value = toFormMetadata(meta, liveValues.value)
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

async function save(values: Record<string, unknown>) {
  if (!serverMeta.value || saving.value) return
  errors.value = {}
  const clientErrors = props.validate?.(values) ?? {}
  if (Object.keys(clientErrors).length) {
    errors.value = clientErrors
    return
  }
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
  <div class="w-full">
    <h1 v-if="!embedded" class="page-title">{{ title }}</h1>

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

    <UiAlert v-if="loadError" variant="danger" class="mb-3" :messages="[t(loadError)]" />

    <TabbedForm
      v-if="metadata"
      :metadata="metadata"
      :model-value="initial"
      :errors="errors"
      :saving="saving"
      @values-change="onValuesChange"
      @save="save"
      @cancel="router.push(backTo)"
    >
      <template #extra><slot name="extra" /></template>
    </TabbedForm>
  </div>
</template>

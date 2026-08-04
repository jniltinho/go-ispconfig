<script setup lang="ts">
// Main Config (System → Main Config): the panel-wide INI in sys_ini, one tab
// per section. Port of interface/web/admin/system_config_edit.php.
//
// Same shape as ServerConfigView — tabs from GET /api/meta/forms/system_config,
// values from GET /api/system/config, one PUT per changed section — because
// the two blobs have the same structure and the same merge semantics. The
// difference is the row it edits (sys_ini, panel-wide) and that a save here is
// felt immediately across every screen: the password policy on this form is
// what every generated credential is measured against.
import { onMounted, ref } from 'vue'
import TabbedForm, { type FormMetadata } from '../../components/TabbedForm.vue'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import { useDialogStore } from '../../stores/dialog'

const { t } = useI18n()
const dialog = useDialogStore()

interface ServerField {
  name: string
  label: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox' | 'checkboxarray' | 'legend'
  datatype: string
  formtype: string
  default?: unknown
  options?: { value: string; label: string }[]
  collapsible?: boolean
}
interface ServerMeta {
  name: string
  title: string
  tabs: { name: string; label: string; fields: ServerField[] }[]
}

const serverMeta = ref<ServerMeta | null>(null)
const metadata = ref<FormMetadata | null>(null)
const initial = ref<Record<string, unknown>>({})
/** Sections exactly as loaded. */
const loaded = ref<Record<string, Record<string, string>>>({})
/**
 * What each section would serialise to before anything was touched. Compared
 * against on save instead of `loaded`: a key absent from the stored INI shows
 * its field default, and sending that would materialise defaults into every
 * section on a save that changed one.
 */
const baseline = ref<Record<string, Record<string, string>>>({})
const loadError = ref('')
const fieldErrors = ref<Record<string, string[]>>({})
const saving = ref(false)

function truthyOption(field: ServerField): string {
  return field.options?.at(-1)?.value ?? 'y'
}
function falsyOption(field: ServerField): string {
  return field.options?.[0]?.value ?? 'n'
}

function toFormMetadata(meta: ServerMeta): FormMetadata {
  return {
    tabs: meta.tabs.map((tab) => ({
      name: tab.name,
      label: t(tab.label),
      fields: tab.fields.map((field) => ({
        name: field.name,
        type: field.type,
        label: t(field.label),
        collapsible: field.collapsible,
        options: field.options?.map((o) => ({ value: o.value, label: t(o.label) })),
      })),
    })),
  }
}

function toFormValues(meta: ServerMeta, sections: Record<string, Record<string, string>>) {
  const values: Record<string, unknown> = {}
  for (const tab of meta.tabs) {
    const section = sections[tab.name] ?? {}
    for (const field of tab.fields) {
      if (field.type === 'legend') continue
      const raw = section[field.name] ?? String(field.default ?? '')
      values[field.name] = field.type === 'checkbox' ? raw === truthyOption(field) : raw
    }
  }
  return values
}

function sectionPayload(
  tab: ServerMeta['tabs'][number],
  values: Record<string, unknown>,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const field of tab.fields) {
    if (field.type === 'legend') continue
    const v = values[field.name]
    if (v === undefined) continue
    out[field.name] = field.type === 'checkbox'
      ? (v ? truthyOption(field) : falsyOption(field))
      : String(v)
  }
  return out
}

function changed(name: string, payload: Record<string, string>): boolean {
  const before = baseline.value[name] ?? {}
  return Object.entries(payload).some(([k, v]) => (before[k] ?? '') !== v)
}

onMounted(async () => {
  try {
    const [meta, sections] = await Promise.all([
      api.get<ServerMeta>('/api/meta/forms/system_config'),
      api.get<Record<string, Record<string, string>>>('/api/system/config'),
    ])
    serverMeta.value = meta
    loaded.value = sections ?? {}
    initial.value = toFormValues(meta, loaded.value)
    baseline.value = Object.fromEntries(
      meta.tabs.map((tab) => [tab.name, sectionPayload(tab, initial.value)]),
    )
    metadata.value = toFormMetadata(meta)
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

async function save(values: Record<string, unknown>) {
  const meta = serverMeta.value
  if (!meta || saving.value) return
  saving.value = true
  loadError.value = ''
  fieldErrors.value = {}
  let savedAny = false
  try {
    for (const tab of meta.tabs) {
      const payload = sectionPayload(tab, values)
      if (!Object.keys(payload).length || !changed(tab.name, payload)) continue
      const stored = await api.put<Record<string, string>>(
        `/api/system/config/${tab.name}`,
        payload,
      )
      loaded.value = { ...loaded.value, [tab.name]: stored }
      baseline.value = { ...baseline.value, [tab.name]: payload }
      savedAny = true
    }
    if (savedAny) dialog.toast(t('sysini.saved'))
  } catch (e) {
    if (e instanceof ApiError && e.status === 422 && e.fields) {
      // Translate the per-field keys so TabbedForm shows them inline and
      // jumps to the offending tab.
      const translated: Record<string, string[]> = {}
      for (const [field, keys] of Object.entries(e.fields)) {
        translated[field] = keys.map((k) => t(k))
      }
      fieldErrors.value = translated
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
    <h1 class="page-title">{{ t('sysini.title') }}</h1>
    <p class="mb-3 text-sm text-text-muted">{{ t('sysini.subtitle') }}</p>

    <UiAlert
      v-if="loadError"
      variant="danger"
      class="mb-3"
      :messages="[t(loadError)]"
      data-test="load-error"
    />

    <TabbedForm
      v-if="metadata"
      :metadata="metadata"
      :model-value="initial"
      :errors="fieldErrors"
      :saving="saving"
      @save="save"
      @cancel="$router.push('/system')"
    />
  </div>
</template>

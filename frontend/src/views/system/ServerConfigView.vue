<script setup lang="ts">
// Server Config editor (System → Server Config), the SPA half of
// spec server-config-sync and the port of the legacy
// interface/web/admin/server_config_edit.php tabbed form.
//
// Tabs come from GET /api/meta/forms/server_config and map 1:1 onto INI
// sections of server.config; values come from GET /api/servers/:id/config.
// Saving PUTs one section per changed tab, because the API merges section by
// section — that is what keeps the keys the panel does not render alive
// across a round trip.
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import TabbedForm, { type FormMetadata } from '../../components/TabbedForm.vue'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

interface ServerField {
  name: string
  label: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox' | 'legend'
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

const serverId = String(route.params.id ?? '1')
const serverMeta = ref<ServerMeta | null>(null)
const metadata = ref<FormMetadata | null>(null)
const initial = ref<Record<string, unknown>>({})
/** Sections exactly as loaded, so save can skip the untouched ones. */
const loaded = ref<Record<string, Record<string, string>>>({})
/**
 * What each section would serialise to before the operator touched anything.
 * Compared against on save instead of `loaded`: a key absent from the stored
 * INI shows its field default, which getconf already applies — sending it
 * would materialise defaults into every section on a save that changed one.
 */
const baseline = ref<Record<string, Record<string, string>>>({})
const serverName = ref('')
const loadError = ref('')
const saved = ref(false)
const saving = ref(false)

/** truthy/falsy return the stored strings behind a checkbox ('y'/'n'). */
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
        options: field.options?.map((o) => ({ value: o.value, label: t(o.label) })),
      })),
    })),
  }
}

// toFormValues maps the INI strings onto the control types: a checkbox holds
// a boolean, everything else the raw string. A key absent from the stored INI
// falls back to the field default, matching what getconf decodes for it.
function toFormValues(meta: ServerMeta, sections: Record<string, Record<string, string>>) {
  const values: Record<string, unknown> = {}
  for (const tab of meta.tabs) {
    const section = sections[tab.name] ?? {}
    for (const field of tab.fields) {
      const raw = section[field.name] ?? String(field.default ?? '')
      values[field.name] = field.type === 'checkbox' ? raw === truthyOption(field) : raw
    }
  }
  return values
}

// sectionPayload converts the submitted values of one tab back to INI
// strings. Every rendered key is sent: the API merges the section, so an
// omitted key would keep its old value while the form claims otherwise.
function sectionPayload(
  tab: ServerMeta['tabs'][number],
  values: Record<string, unknown>,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const field of tab.fields) {
    const v = values[field.name]
    if (v === undefined) continue
    out[field.name] = field.type === 'checkbox'
      ? (v ? truthyOption(field) : falsyOption(field))
      : String(v)
  }
  return out
}

/** changed reports whether a section differs from what the form loaded with. */
function changed(name: string, payload: Record<string, string>): boolean {
  const before = baseline.value[name] ?? {}
  return Object.entries(payload).some(([k, v]) => (before[k] ?? '') !== v)
}

onMounted(async () => {
  try {
    const [meta, sections, server] = await Promise.all([
      api.get<ServerMeta>('/api/meta/forms/server_config'),
      api.get<Record<string, Record<string, string>>>(`/api/servers/${serverId}/config`),
      api.get<{ server_name?: string }>(`/api/server/${serverId}`).catch(
        () => ({}) as { server_name?: string },
      ),
    ])
    serverMeta.value = meta
    loaded.value = sections ?? {}
    serverName.value = server.server_name ?? ''
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
  saved.value = false
  loadError.value = ''
  try {
    for (const tab of meta.tabs) {
      const payload = sectionPayload(tab, values)
      if (!Object.keys(payload).length || !changed(tab.name, payload)) continue
      const stored = await api.put<Record<string, string>>(
        `/api/servers/${serverId}/config/${tab.name}`,
        payload,
      )
      loaded.value = { ...loaded.value, [tab.name]: stored }
      baseline.value = { ...baseline.value, [tab.name]: payload }
    }
    saved.value = true
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="w-full">
    <h1 class="page-title">{{ t('srvcfg.title') }}</h1>
    <p v-if="serverName" class="mb-3 text-sm text-muted" data-test="server-name">
      {{ t('srvcfg.config_for') }} {{ serverName }}
    </p>

    <UiAlert
      v-if="loadError"
      variant="danger"
      class="mb-3"
      :messages="[t(loadError)]"
      data-test="load-error"
    />
    <UiAlert
      v-if="saved"
      variant="info"
      class="mb-3"
      :messages="[t('srvcfg.saved')]"
      data-test="saved"
    />

    <TabbedForm
      v-if="metadata"
      :metadata="metadata"
      :model-value="initial"
      :saving="saving"
      @save="save"
      @cancel="router.push('/system/server-config')"
    />
  </div>
</template>

<script setup lang="ts">
// Embedded DNS record editor: grid over /api/dns/zones/{id}/records (ordered
// type,name by the API) plus a metadata-driven add/edit dialog whose fields
// and client-side validation come from /api/dns/record-types — the same
// source of truth the API validates against. Every mutation refreshes the
// grid and the bumped SOA serial.
import { computed, onMounted, ref } from 'vue'
import { utilityIcons } from '../../icons'
import { api, ApiError } from '../../api'
import UiAlert from '../../components/UiAlert.vue'
import { useI18n } from '../../i18n'

const props = defineProps<{ zoneId: string }>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()

/** RecordType mirrors api.DNSRecordType. */
interface RecordType {
  type: string
  stored_type: string
  name_regex: string
  name_required: boolean
  data_kind: string
  data_regex?: string
  data_prefix?: string
  data_not_contains?: string[]
  data_label: string
  aux_used: boolean
  aux_label?: string
  aux_default: number
  ttl_default: number
}

type RecordRow = Record<string, unknown>

const types = ref<RecordType[]>([])
const rows = ref<RecordRow[]>([])
const serial = ref('')
const error = ref('')

// Dialog state.
const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const form = ref({ type: 'A', name: '', data: '', aux: '0', ttl: '3600', active: true })
const fieldErrors = ref<Record<string, string[]>>({})
const saving = ref(false)

const currentType = computed(
  () => types.value.find((rt) => rt.type === form.value.type) ?? types.value[0],
)

async function load() {
  error.value = ''
  try {
    const [zone, records] = await Promise.all([
      api.get<Record<string, unknown>>(`/api/dns/zones/${props.zoneId}`),
      api.get<RecordRow[]>(`/api/dns/zones/${props.zoneId}/records`),
    ])
    serial.value = String(zone.serial ?? '')
    rows.value = records ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

onMounted(async () => {
  try {
    types.value = (await api.get<RecordType[]>('/api/dns/record-types')) ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
  await load()
})

function openAdd() {
  editingId.value = null
  const rt = types.value[0]
  form.value = {
    type: rt?.type ?? 'A',
    name: '',
    data: '',
    aux: String(rt?.aux_default ?? 0),
    ttl: String(rt?.ttl_default ?? 3600),
    active: true,
  }
  fieldErrors.value = {}
  dialogOpen.value = true
}

function openEdit(row: RecordRow) {
  editingId.value = Number(row.id)
  form.value = {
    type: String(row.type),
    name: String(row.name ?? ''),
    data: String(row.data ?? ''),
    aux: String(row.aux ?? 0),
    ttl: String(row.ttl ?? 3600),
    active: row.active === 'Y',
  }
  fieldErrors.value = {}
  dialogOpen.value = true
}

function onTypeChange() {
  const rt = currentType.value
  if (!rt) return
  if (editingId.value === null) {
    form.value.aux = String(rt.aux_default)
    form.value.ttl = String(rt.ttl_default)
  }
}

// Client-side validation mirroring the API metadata rules; the API remains
// the authority (422 responses are mapped onto the same fields).
function validate(): boolean {
  const rt = currentType.value
  const errs: Record<string, string[]> = {}
  if (!rt) return false
  const { name, data, aux, ttl } = form.value
  if (rt.name_required && name === '') errs.name = [t('name_error_empty')]
  else if (!new RegExp(rt.name_regex).test(name)) errs.name = [t('name_error_regex')]
  if (data === '') {
    errs.data = [t('data_error_empty')]
  } else {
    if (rt.data_kind === 'ipv4' && !/^\d{1,3}(\.\d{1,3}){3}$/.test(data))
      errs.data = [t('ip_error_wrong')]
    if (rt.data_kind === 'ipv6' && !/^[0-9a-fA-F:.]+$/.test(data))
      errs.data = [t('ip_error_wrong')]
    if (rt.data_regex && !new RegExp(rt.data_regex).test(data))
      errs.data = [t('data_error_regex')]
    if (rt.data_prefix && !data.startsWith(rt.data_prefix))
      errs.data = [t('data_error_regex')]
    for (const bad of rt.data_not_contains ?? []) {
      if (data.includes(bad)) errs.data = [t('data_error_use_dedicated_form')]
    }
  }
  const auxNum = Number(aux)
  if (rt.aux_used && (!Number.isInteger(auxNum) || auxNum < 0 || auxNum > 65535))
    errs.aux = [t('aux_error_range')]
  const ttlNum = Number(ttl)
  if (!Number.isInteger(ttlNum) || ttlNum < 60) errs.ttl = [t('ttl_range_error')]
  fieldErrors.value = errs
  return Object.keys(errs).length === 0
}

async function save() {
  if (!validate()) return
  saving.value = true
  try {
    const payload = {
      type: form.value.type,
      name: form.value.name,
      data: form.value.data,
      aux: Number(form.value.aux),
      ttl: Number(form.value.ttl),
      active: form.value.active ? 'Y' : 'N',
    }
    if (editingId.value === null) {
      await api.post(`/api/dns/zones/${props.zoneId}/records`, payload)
    } else {
      await api.put(`/api/dns/records/${editingId.value}`, payload)
    }
    dialogOpen.value = false
    await load()
    emit('changed')
  } catch (e) {
    if (e instanceof ApiError && e.status === 422 && e.fields) {
      const translated: Record<string, string[]> = {}
      for (const [field, keys] of Object.entries(e.fields)) {
        translated[field] = keys.map((key) => t(key))
      }
      fieldErrors.value = translated
    } else {
      error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    }
  } finally {
    saving.value = false
  }
}

async function remove(row: RecordRow) {
  if (!confirm(t('sites.confirm_delete'))) return
  try {
    await api.delete(`/api/dns/records/${row.id}`)
    await load()
    emit('changed')
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

async function toggleActive(row: RecordRow) {
  try {
    await api.put(`/api/dns/records/${row.id}`, { active: row.active === 'Y' ? 'N' : 'Y' })
    await load()
    emit('changed')
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center justify-between">
      <button
        type="button"
        data-test="add-record"
        class="btn btn-success px-4 py-2"
        @click="openAdd"
      >
        {{ t('dns.add_record') }}
      </button>
      <span class="text-xs text-text/70" data-test="zone-serial">
        {{ t('dns.serial') }}: {{ serial }}
      </span>
    </div>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" />

    <div class="overflow-x-auto border border-border bg-surface">
      <table class="w-full border-collapse text-sm" data-test="record-grid">
        <thead class="bg-thead text-white">
          <tr>
            <th class="px-3 py-2.5 text-left text-xs font-bold uppercase">{{ t('dns.col.rr_name') }}</th>
            <th class="px-3 py-2.5 text-left text-xs font-bold uppercase">{{ t('dns.col.rr_type') }}</th>
            <th class="px-3 py-2.5 text-left text-xs font-bold uppercase">{{ t('dns.col.rr_data') }}</th>
            <th class="px-3 py-2.5 text-left text-xs font-bold uppercase">{{ t('dns.col.rr_priority') }}</th>
            <th class="px-3 py-2.5 text-left text-xs font-bold uppercase">{{ t('dns.col.rr_ttl') }}</th>
            <th class="px-3 py-2.5 text-left text-xs font-bold uppercase">{{ t('sites.col.active') }}</th>
            <th class="px-3 py-2.5 text-right text-xs font-bold uppercase">{{ t('table.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="rows.length === 0">
            <td colspan="7" class="px-3 py-6 text-center text-text">{{ t('table.empty') }}</td>
          </tr>
          <tr
            v-for="row in rows"
            :key="String(row.id)"
            class="cursor-pointer border-t border-border odd:bg-bg hover:bg-info"
            @click="openEdit(row)"
          >
            <td class="px-3 py-2">{{ row.name }}</td>
            <td class="px-3 py-2">{{ row.type }}</td>
            <td class="max-w-md truncate px-3 py-2">{{ row.data }}</td>
            <td class="px-3 py-2">{{ row.aux }}</td>
            <td class="px-3 py-2">{{ row.ttl }}</td>
            <td class="px-3 py-2" @click.stop>
              <input
                type="checkbox"
                :checked="row.active === 'Y'"
                :title="t('active_txt')"
                :aria-label="t('active_txt')"
                @change="toggleActive(row)"
              />
            </td>
            <td class="px-3 py-2 text-right whitespace-nowrap" @click.stop>
              <button
                type="button"
                :title="t('sites.edit')"
                :aria-label="t('sites.edit')"
                class="border border-border bg-surface p-1 hover:bg-info"
                @click="openEdit(row)"
              >
                <component :is="utilityIcons.edit" :size="14" />
              </button>
              <button
                type="button"
                :title="t('sites.delete')"
                :aria-label="t('sites.delete')"
                data-test="delete-record"
                class="ml-1 border border-danger-border bg-danger p-1 text-danger-text"
                @click="remove(row)"
              >
                <component :is="utilityIcons.delete" :size="14" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Add/edit dialog -->
    <div
      v-if="dialogOpen"
      class="fixed inset-0 z-40 flex items-start justify-center bg-black/40 pt-24"
      @click.self="dialogOpen = false"
    >
      <div class="w-full max-w-lg border border-border bg-surface" data-test="record-dialog">
        <div class="border-b border-border bg-bg px-4 py-2 text-sm font-bold">
          {{ editingId === null ? t('dns.add_record') : t('dns.edit_record') }}
        </div>
        <form class="space-y-3 px-4 py-4" @submit.prevent="save">
          <div class="flex items-start gap-3">
            <label for="rr-type" class="w-32 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
              {{ t('dns.col.rr_type') }}
            </label>
            <select
              id="rr-type"
              v-model="form.type"
              class="flex-1 border border-border bg-surface px-3 py-1.5 text-sm outline-none"
              :disabled="editingId !== null"
              @change="onTypeChange"
            >
              <option v-for="rt in types" :key="rt.type" :value="rt.type">{{ rt.type }}</option>
            </select>
          </div>
          <div class="flex items-start gap-3">
            <label for="rr-name" class="w-32 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
              {{ t('dns.col.rr_name') }}
            </label>
            <div class="flex-1">
              <input
                id="rr-name"
                v-model="form.name"
                type="text"
                :placeholder="t('dns.name_placeholder')"
                class="w-full border border-border bg-surface px-3 py-1.5 text-sm outline-none"
                :class="{ 'border-danger-border': fieldErrors.name?.length }"
              />
              <p v-if="fieldErrors.name?.length" class="mt-1 text-xs text-danger-text">
                {{ fieldErrors.name.join(', ') }}
              </p>
            </div>
          </div>
          <div class="flex items-start gap-3">
            <label for="rr-data" class="w-32 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
              {{ currentType ? t(currentType.data_label) : t('dns.col.rr_data') }}
            </label>
            <div class="flex-1">
              <input
                id="rr-data"
                v-model="form.data"
                type="text"
                class="w-full border border-border bg-surface px-3 py-1.5 text-sm outline-none"
                :class="{ 'border-danger-border': fieldErrors.data?.length }"
              />
              <p v-if="fieldErrors.data?.length" class="mt-1 text-xs text-danger-text">
                {{ fieldErrors.data.join(', ') }}
              </p>
            </div>
          </div>
          <div v-if="currentType?.aux_used" class="flex items-start gap-3">
            <label for="rr-aux" class="w-32 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
              {{ currentType?.aux_label ? t(currentType.aux_label) : t('dns.col.rr_priority') }}
            </label>
            <div class="flex-1">
              <input
                id="rr-aux"
                v-model="form.aux"
                type="number"
                class="w-32 border border-border bg-surface px-3 py-1.5 text-sm outline-none"
                :class="{ 'border-danger-border': fieldErrors.aux?.length }"
              />
              <p v-if="fieldErrors.aux?.length" class="mt-1 text-xs text-danger-text">
                {{ fieldErrors.aux.join(', ') }}
              </p>
            </div>
          </div>
          <div class="flex items-start gap-3">
            <label for="rr-ttl" class="w-32 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
              {{ t('dns.col.rr_ttl') }}
            </label>
            <div class="flex-1">
              <input
                id="rr-ttl"
                v-model="form.ttl"
                type="number"
                class="w-32 border border-border bg-surface px-3 py-1.5 text-sm outline-none"
                :class="{ 'border-danger-border': fieldErrors.ttl?.length }"
              />
              <p v-if="fieldErrors.ttl?.length" class="mt-1 text-xs text-danger-text">
                {{ fieldErrors.ttl.join(', ') }}
              </p>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <span class="w-32 shrink-0 text-right text-sm font-semibold after:content-[':']">
              {{ t('active_txt') }}
            </span>
            <input v-model="form.active" type="checkbox" />
          </div>
          <div class="flex justify-end gap-2 border-t border-border pt-3">
            <button
              type="button"
              class="btn btn-default px-6"
              @click="dialogOpen = false"
            >
              {{ t('form.cancel') }}
            </button>
            <button
              type="submit"
              data-test="record-save"
              :disabled="saving"
              class="btn btn-success px-6"
            >
              {{ t('form.save') }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

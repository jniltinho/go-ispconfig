<script setup lang="ts">
// System → Remote Users: the API tokens that authenticate automation
// (spec add-api-tokens). List, mint, revoke, re-enable and delete.
//
// The minted secret is displayed exactly once, right after creation, and is
// held only in this component's state — reloading or navigating away loses it
// for good, because no endpoint can return it again.
import { computed, onMounted, ref } from 'vue'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()

interface TokenRow {
  id: number
  label: string
  owner: string
  scopes: string[]
  allowed_ips: string
  enabled: boolean
  expires_at?: string
  last_used_at?: string
}

const rows = ref<TokenRow[]>([])
const scopeOptions = ref<string[]>([])
const error = ref('')
const loading = ref(false)
const saving = ref(false)

// The one-time secret. Never persisted, never re-fetchable.
const minted = ref('')

const showForm = ref(false)
const form = ref({ label: '', owner: '', scopes: [] as string[], allowed_ips: '', expires_at: '' })
const fieldErrors = ref<Record<string, string[]>>({})

const canSubmit = computed(() => form.value.label.trim() !== '' && form.value.scopes.length > 0)

async function load() {
  loading.value = true
  error.value = ''
  try {
    rows.value = await api.get<TokenRow[]>('/api/tokens')
    scopeOptions.value = await api.get<string[]>('/api/tokens/scopes')
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  } finally {
    loading.value = false
  }
}

onMounted(load)

function toggleScope(scope: string, on: boolean) {
  const set = new Set(form.value.scopes)
  if (on) set.add(scope)
  else set.delete(scope)
  form.value.scopes = scopeOptions.value.filter((s) => set.has(s))
}

function openForm() {
  form.value = { label: '', owner: '', scopes: [], allowed_ips: '', expires_at: '' }
  fieldErrors.value = {}
  minted.value = ''
  showForm.value = true
}

async function create() {
  if (!canSubmit.value || saving.value) return
  saving.value = true
  error.value = ''
  fieldErrors.value = {}
  try {
    const body: Record<string, unknown> = {
      label: form.value.label.trim(),
      scopes: form.value.scopes,
      allowed_ips: form.value.allowed_ips.trim(),
    }
    if (form.value.owner.trim()) body.owner = form.value.owner.trim()
    // The API wants RFC3339; the date input gives YYYY-MM-DD.
    if (form.value.expires_at) body.expires_at = `${form.value.expires_at}T00:00:00Z`

    const created = await api.post<{ token: string }>('/api/tokens', body)
    minted.value = created.token
    showForm.value = false
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.status === 422 && e.fields) {
      fieldErrors.value = e.fields
    } else {
      error.value = e instanceof ApiError ? e.key : 'error.request_failed'
    }
  } finally {
    saving.value = false
  }
}

async function setEnabled(row: TokenRow, enabled: boolean) {
  try {
    await api.put(`/api/tokens/${row.id}`, { enabled })
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

async function remove(row: TokenRow) {
  if (!confirm(t('tokens.confirm_delete'))) return
  try {
    await api.delete(`/api/tokens/${row.id}`)
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
}

async function copyMinted() {
  try {
    await navigator.clipboard.writeText(minted.value)
  } catch {
    // Clipboard unavailable (insecure context): the value is on screen and
    // selectable, which is the fallback that always works.
  }
}
</script>

<template>
  <div class="w-full">
    <h1 class="page-title">{{ t('tokens.title') }}</h1>

    <UiAlert v-if="error" variant="danger" class="mb-3" :messages="[t(error)]" data-test="tokens-error" />

    <!-- The one-time secret. Shown once; there is no way back to it. -->
    <div
      v-if="minted"
      class="mb-4 border border-border bg-info p-3"
      data-test="minted-token"
    >
      <p class="mb-2 text-sm font-bold">{{ t('tokens.minted_warning') }}</p>
      <div class="flex items-center gap-2">
        <code class="flex-1 select-all break-all border border-border bg-surface px-2 py-1 text-sm">{{ minted }}</code>
        <button type="button" class="btn btn-success px-3 py-1 text-sm" @click="copyMinted">
          {{ t('tokens.copy') }}
        </button>
        <button type="button" class="border border-border px-3 py-1 text-sm" @click="minted = ''">
          {{ t('tokens.dismiss') }}
        </button>
      </div>
    </div>

    <button
      v-if="!showForm"
      type="button"
      class="my-3 btn btn-success px-4 py-2"
      data-test="tokens-add"
      @click="openForm"
    >
      {{ t('tokens.add') }}
    </button>

    <!-- Create form -->
    <form
      v-if="showForm"
      class="mb-4 border border-border bg-surface p-4"
      data-test="tokens-form"
      @submit.prevent="create"
    >
      <div class="mb-3 flex items-start gap-3">
        <label for="token-label" class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']">
          {{ t('tokens.label') }}
        </label>
        <div class="flex-1">
          <input
            id="token-label"
            v-model="form.label"
            type="text"
            class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link"
          />
          <p v-if="fieldErrors.label" class="mt-1 text-sm text-danger-text">
            {{ fieldErrors.label.map((k) => t(k)).join(", ") }}
          </p>
        </div>
      </div>

      <div class="mb-3 flex items-start gap-3">
        <label for="token-owner" class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']">
          {{ t('tokens.owner') }}
        </label>
        <div class="flex-1">
          <input
            id="token-owner"
            v-model="form.owner"
            type="text"
            :placeholder="t('tokens.owner_placeholder')"
            class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link"
          />
          <p v-if="fieldErrors.owner" class="mt-1 text-sm text-danger-text">
            {{ fieldErrors.owner.map((k) => t(k)).join(", ") }}
          </p>
        </div>
      </div>

      <div class="mb-3 flex items-start gap-3">
        <span class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']">
          {{ t('tokens.scopes') }}
        </span>
        <div class="flex-1">
          <div class="flex flex-wrap gap-x-4 gap-y-1">
            <label v-for="s in scopeOptions" :key="s" class="flex items-center gap-1 text-sm">
              <input
                type="checkbox"
                :value="s"
                :checked="form.scopes.includes(s)"
                @change="toggleScope(s, ($event.target as HTMLInputElement).checked)"
              />
              <code>{{ s }}</code>
            </label>
          </div>
          <p class="mt-1 text-sm text-text-muted">{{ t('tokens.scopes_hint') }}</p>
          <p v-if="fieldErrors.scopes" class="mt-1 text-sm text-danger-text">
            {{ fieldErrors.scopes.map((k) => t(k)).join(", ") }}
          </p>
        </div>
      </div>

      <div class="mb-3 flex items-start gap-3">
        <label for="token-ips" class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']">
          {{ t('tokens.allowed_ips') }}
        </label>
        <div class="flex-1">
          <input
            id="token-ips"
            v-model="form.allowed_ips"
            type="text"
            placeholder="10.0.0.0/8, 203.0.113.5"
            class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link"
          />
          <p v-if="fieldErrors.allowed_ips" class="mt-1 text-sm text-danger-text">
            {{ fieldErrors.allowed_ips.map((k) => t(k)).join(", ") }}
          </p>
        </div>
      </div>

      <div class="mb-4 flex items-start gap-3">
        <label for="token-expires" class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']">
          {{ t('tokens.expires') }}
        </label>
        <div class="flex-1">
          <input
            id="token-expires"
            v-model="form.expires_at"
            type="date"
            class="border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link"
          />
          <p v-if="fieldErrors.expires_at" class="mt-1 text-sm text-danger-text">
            {{ fieldErrors.expires_at.map((k) => t(k)).join(", ") }}
          </p>
        </div>
      </div>

      <div class="flex justify-end gap-2">
        <button type="button" class="border border-border px-6 py-1" @click="showForm = false">
          {{ t('tokens.cancel') }}
        </button>
        <button type="submit" class="btn btn-success px-8" :disabled="!canSubmit || saving">
          {{ t('tokens.create') }}
        </button>
      </div>
    </form>

    <!-- List -->
    <table class="w-full border border-border bg-surface text-sm">
      <thead>
        <tr class="bg-thead text-left text-white">
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.label') }}</th>
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.owner') }}</th>
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.scopes') }}</th>
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.ips') }}</th>
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.expires') }}</th>
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.last_used') }}</th>
          <th class="px-3 py-2.5 text-xs font-bold uppercase">{{ t('tokens.col.enabled') }}</th>
          <th class="px-3 py-2.5 text-right text-xs font-bold uppercase">{{ t('tokens.col.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!loading && rows.length === 0">
          <td colspan="8" class="px-3 py-4 text-center text-text-muted">{{ t('tokens.empty') }}</td>
        </tr>
        <tr v-for="row in rows" :key="row.id" class="border-t border-border">
          <td class="px-3 py-2">{{ row.label }}</td>
          <td class="px-3 py-2">{{ row.owner }}</td>
          <td class="px-3 py-2"><code>{{ row.scopes.join(', ') }}</code></td>
          <td class="px-3 py-2">{{ row.allowed_ips || '—' }}</td>
          <td class="px-3 py-2">{{ row.expires_at || t('tokens.never') }}</td>
          <td class="px-3 py-2">{{ row.last_used_at || t('tokens.never_used') }}</td>
          <td class="px-3 py-2">{{ row.enabled ? t('yes_txt') : t('no_txt') }}</td>
          <td class="px-3 py-2 text-right whitespace-nowrap">
            <button
              type="button"
              class="border border-border bg-surface px-2 py-1 text-xs hover:bg-info"
              :data-test="`toggle-${row.id}`"
              @click="setEnabled(row, !row.enabled)"
            >
              {{ row.enabled ? t('tokens.revoke') : t('tokens.enable') }}
            </button>
            <button
              type="button"
              class="ml-1 border border-danger-border bg-danger px-2 py-1 text-xs text-danger-text"
              :data-test="`delete-${row.id}`"
              @click="remove(row)"
            >
              {{ t('tokens.delete') }}
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <p class="mt-3 text-sm text-text-muted">{{ t('tokens.jwt_note') }}</p>
  </div>
</template>

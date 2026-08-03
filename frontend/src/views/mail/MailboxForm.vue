<script setup lang="ts">
// Dedicated mailbox form (legacy mail_user parity): the email is edited as
// local-part + "@" + a select of primary mail domains and composed into the
// full address on save; the quota is edited in MB (0 = unlimited) and
// converted to bytes for the API; password has generate / strength / repeat
// controls; the spamfilter policy is synced to spamfilter_users like
// mail_user_edit.php does. Server-derived columns (maildir, uid, ...) are
// never rendered — the API derives and protects them.
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '../../components/UiAlert.vue'
import { api, ApiError } from '../../api'
import { useI18n } from '../../i18n'
import { useAuthStore } from '../../stores/auth'

const props = defineProps<{ id?: string }>()
const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const isAdmin = computed(() => auth.typ === 'admin')

const MB = 1024 * 1024

const form = reactive({
  name: '',
  email_local: '',
  email_domain: '',
  password: '',
  repeat_password: '',
  quota: '0',
  cc: '',
  sender_cc: '',
  imap_prefix: '',
  policy: '0',
  postfix: true,
  disablesmtp: false,
  disabledeliver: false,
  greylisting: false,
  disableimap: false,
  disablepop3: false,
  autoresponder: false,
  autoresponder_subject: 'Out of office reply',
  autoresponder_text: '',
  autoresponder_start_date: '',
  autoresponder_end_date: '',
  move_junk: 'y',
  forward_in_lda: false,
  custom_mailfilter: '',
  purge_trash_days: '0',
  purge_junk_days: '0',
  backup_interval: 'none',
  backup_copies: '1',
})

// PHP tab order: Mailbox | Autoresponder | Mail Filter | Custom Rules | Backup.
// Custom Rules is admin-only (mail_user.tform.php: typ == admin).
const allTabs = ['mailuser', 'autoresponder', 'filter_records', 'mailfilter', 'backup'] as const
type MailboxTab = (typeof allTabs)[number]
const tabLabelKey: Record<MailboxTab, string> = {
  mailuser: 'mailbox_txt',
  autoresponder: 'autoresponder_txt',
  filter_records: 'mail_filter_txt',
  mailfilter: 'custom_rules_txt',
  backup: 'backup_tab_txt',
}
const tabs = computed(() =>
  allTabs.filter((tab) => tab !== 'mailfilter' || isAdmin.value),
)
const activeTab = ref<string>('mailuser')
const domains = ref<string[]>([])
const policies = ref<{ value: string; label: string }[]>([])
const policiesAvailable = ref(false)
/** Existing spamfilter_users row of this email (id + policy + email for rename). */
const spamUser = ref<{ id: number; policy_id: number; email: string } | null>(null)
const errors = ref<Record<string, string[]>>({})
const loadError = ref('')
const datalogState = ref('')
const datalogError = ref('')
const saving = ref(false)
const showPassword = ref(false)

// Simple zxcvbn-free strength score (0-4): length + character classes,
// same spirit as the legacy pass_check() bar.
const strength = computed(() => {
  const pw = form.password
  if (!pw) return -1
  let s = 0
  if (pw.length >= 8) s++
  if (pw.length >= 12) s++
  if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) s++
  if (/\d/.test(pw)) s++
  if (/[^a-zA-Z0-9]/.test(pw)) s++
  return Math.min(4, s)
})
const strengthKeys = [
  'mail.pw_strength_0',
  'mail.pw_strength_1',
  'mail.pw_strength_2',
  'mail.pw_strength_3',
  'mail.pw_strength_4',
]
const strengthColors = ['bg-danger-border', 'bg-danger-border', 'bg-link', 'bg-success', 'bg-success']
const passwordsMismatch = computed(
  () => form.repeat_password !== '' && form.password !== form.repeat_password,
)

function generatePassword() {
  // No ambiguous chars (0/O, 1/l/I); 12 chars scores "strong".
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789'
  const buf = new Uint32Array(12)
  crypto.getRandomValues(buf)
  const pw = Array.from(buf, (n) => chars[n % chars.length]).join('')
  form.password = pw
  form.repeat_password = pw
  showPassword.value = true
}

onMounted(async () => {
  try {
    const res = await api.get<{ items: { domain: string }[] }>('/api/mail/domains?limit=1000')
    domains.value = res.items.map((r) => r.domain).sort()
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
    return
  }
  try {
    const res = await api.get<{ items: { id: number; policy_name: string }[] }>(
      '/api/mail/spamfilter/policies?limit=1000',
    )
    policies.value = res.items.map((p) => ({ value: String(p.id), label: p.policy_name }))
    policiesAvailable.value = true
  } catch {
    // Non-admin (403) or lookup failure: hide the policy select.
  }
  if (!props.id) {
    form.email_domain = domains.value[0] ?? ''
    return
  }
  try {
    const rec = await api.get<Record<string, unknown>>(`/api/mail/mailboxes/${props.id}`)
    datalogState.value = String(rec._datalog_state ?? '')
    datalogError.value = String(rec._datalog_error ?? '')
    const email = String(rec.email ?? '')
    const at = email.lastIndexOf('@')
    form.email_local = at >= 0 ? email.slice(0, at) : email
    form.email_domain = at >= 0 ? email.slice(at + 1) : ''
    if (form.email_domain && !domains.value.includes(form.email_domain)) {
      domains.value = [...domains.value, form.email_domain].sort()
    }
    form.name = String(rec.name ?? '')
    const quota = Number(rec.quota ?? 0)
    form.quota = String(quota > 0 ? Math.round(quota / MB) : 0)
    form.cc = String(rec.cc ?? '')
    form.sender_cc = String(rec.sender_cc ?? '')
    form.imap_prefix = String(rec.imap_prefix ?? '')
    for (const k of [
      'postfix', 'disablesmtp', 'disabledeliver', 'greylisting', 'disableimap',
      'disablepop3', 'autoresponder', 'forward_in_lda',
    ] as const) {
      form[k] = String(rec[k] ?? 'n') === 'y'
    }
    form.autoresponder_subject = String(rec.autoresponder_subject ?? '')
    form.autoresponder_text = String(rec.autoresponder_text ?? '')
    form.autoresponder_start_date = String(rec.autoresponder_start_date ?? '')
    form.autoresponder_end_date = String(rec.autoresponder_end_date ?? '')
    form.move_junk = String(rec.move_junk ?? 'y')
    form.custom_mailfilter = String(rec.custom_mailfilter ?? '')
    form.purge_trash_days = String(rec.purge_trash_days ?? '0')
    form.purge_junk_days = String(rec.purge_junk_days ?? '0')
    form.backup_interval = String(rec.backup_interval ?? 'none')
    form.backup_copies = String(rec.backup_copies ?? '1')
    if (policiesAvailable.value) {
      const su = await api.get<{ items: { id: number; email: string; policy_id: number }[] }>(
        `/api/mail/spamfilter/users?email=${encodeURIComponent(email)}&limit=100`,
      )
      const match = su.items.find((u) => u.email === email)
      if (match) {
        spamUser.value = { id: match.id, policy_id: match.policy_id, email: match.email }
        form.policy = String(match.policy_id)
      }
    }
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
  }
})

// syncSpamfilterPolicy mirrors mail_user_edit.php onAfterInsert/Update:
// update the existing spamfilter_users row when the policy or mailbox
// email changed, or insert one (priority 7, local Y) when a policy is
// picked for the first time. Best-effort: the mailbox itself is already saved.
async function syncSpamfilterPolicy(saved: Record<string, unknown>) {
  const policyId = Number(form.policy)
  const email = String(saved.email ?? '')
  if (spamUser.value) {
    const patch: Record<string, unknown> = {}
    if (policyId !== spamUser.value.policy_id) patch.policy_id = policyId
    // Rename: keep spamfilter_users.email in lockstep (PHP onAfterUpdate).
    if (email && email !== spamUser.value.email) {
      patch.email = email
      patch.fullname = email
    }
    if (Object.keys(patch).length) {
      await api.put(`/api/mail/spamfilter/users/${spamUser.value.id}`, patch)
    }
  } else if (policyId > 0) {
    await api.post('/api/mail/spamfilter/users', {
      server_id: Number(saved.server_id ?? 0),
      priority: 7,
      policy_id: policyId,
      email,
      fullname: email,
      local: 'Y',
    })
  }
}

async function save() {
  if (saving.value) return
  const clientErrors: Record<string, string[]> = {}
  if (!form.email_local.trim()) clientErrors.email = [t('email_error_empty')]
  if (!form.email_domain) clientErrors.email = [t('mail.mailbox_domain_error_empty')]
  if (!props.id && !form.password) clientErrors.password = [t('password_error_empty')]
  if (form.password !== form.repeat_password) {
    clientErrors.repeat_password = [t('mail.password_mismatch')]
  }
  if (!/^\d+$/.test(form.quota.trim())) clientErrors.quota = [t('quota_error_isint')]
  errors.value = clientErrors
  if (Object.keys(clientErrors).length) return

  const yn = (v: boolean) => (v ? 'y' : 'n')
  const payload: Record<string, unknown> = {
    email: `${form.email_local.trim()}@${form.email_domain}`,
    name: form.name,
    quota: Number(form.quota.trim()) * MB,
    cc: form.cc,
    sender_cc: form.sender_cc,
    postfix: yn(form.postfix),
    disablesmtp: yn(form.disablesmtp),
    disabledeliver: yn(form.disabledeliver),
    greylisting: yn(form.greylisting),
    disableimap: yn(form.disableimap),
    disablepop3: yn(form.disablepop3),
    autoresponder: yn(form.autoresponder),
    autoresponder_subject: form.autoresponder_subject,
    autoresponder_text: form.autoresponder_text,
    move_junk: form.move_junk,
    forward_in_lda: yn(form.forward_in_lda),
    purge_trash_days: Number(form.purge_trash_days || '0'),
    purge_junk_days: Number(form.purge_junk_days || '0'),
    backup_interval: form.backup_interval,
    backup_copies: Number(form.backup_copies || '1'),
  }
  if (form.password) payload.password = form.password
  // PHP: imap_prefix + custom_mailfilter are admin-only (applyBody ignores AdminOnly for clients).
  if (isAdmin.value) {
    payload.imap_prefix = form.imap_prefix
    payload.custom_mailfilter = form.custom_mailfilter
  }
  if (form.autoresponder_start_date) payload.autoresponder_start_date = form.autoresponder_start_date
  if (form.autoresponder_end_date) payload.autoresponder_end_date = form.autoresponder_end_date

  saving.value = true
  try {
    const saved = props.id
      ? await api.put<Record<string, unknown>>(`/api/mail/mailboxes/${props.id}`, payload)
      : await api.post<Record<string, unknown>>('/api/mail/mailboxes', payload)
    if (policiesAvailable.value) await syncSpamfilterPolicy(saved ?? payload)
    router.push('/mail/mailboxes')
  } catch (e) {
    if (e instanceof ApiError && e.status === 422 && e.fields) {
      const translated: Record<string, string[]> = {}
      for (const [field, keys] of Object.entries(e.fields)) {
        translated[field] = keys.map((key) => t(key))
      }
      errors.value = translated
      // Jump to the tab holding the offending field (dates are the only
      // validated fields outside the main tab).
      const dateFields = ['autoresponder_start_date', 'autoresponder_end_date']
      activeTab.value = Object.keys(translated).every((f) => dateFields.includes(f))
        ? 'autoresponder'
        : 'mailuser'
    } else {
      loadError.value = e instanceof ApiError ? e.key : 'error.request_failed'
    }
  } finally {
    saving.value = false
  }
}

/** fieldLabel resolves an error field to its display label for the summary. */
const labelKeys: Record<string, string> = {
  email: 'email_txt', password: 'password_txt', repeat_password: 'mail.repeat_password',
  quota: 'mail.quota_mb', cc: 'cc_txt', sender_cc: 'sender_cc_txt', name: 'name_txt',
  imap_prefix: 'mail.imap_prefix',
  autoresponder_start_date: 'autoresponder_start_date_txt',
  autoresponder_end_date: 'autoresponder_end_date_txt',
}
const fieldLabel = (name: string) => (labelKeys[name] ? t(labelKeys[name]) : name)

const errorList = () => Object.entries(errors.value).filter(([, msgs]) => msgs.length > 0)

const inputClass =
  'w-full max-w-md border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link'
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('mailuser_edit_title') }}</h1>

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

    <form class="border border-border bg-surface" @submit.prevent="save">
      <!-- Flat tabs (TabbedForm styling) — legacy order: Mailbox | Autoresponder | Mail Filter | Custom Rules | Backup -->
      <div role="tablist" class="flex flex-wrap border-b border-border bg-bg">
        <button
          v-for="tab in tabs"
          :key="tab"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab"
          :aria-controls="`tabpanel-${tab}`"
          class="border-b-2 px-4 py-2 text-sm font-semibold"
          :class="
            activeTab === tab
              ? 'border-link text-link'
              : 'border-transparent text-muted hover:text-fg'
          "
          @click="activeTab = tab"
        >
          {{ t(tabLabelKey[tab]) }}
        </button>
      </div>

      <UiAlert
        v-if="errorList().length"
        variant="danger"
        class="m-4"
        :messages="errorList().map(([field, msgs]) => `${fieldLabel(field)}: ${msgs.join(', ')}`)"
      />

      <!-- Mailbox tab -->
      <div v-show="activeTab === 'mailuser'" id="tabpanel-mailuser" role="tabpanel" class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="field-name" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('name_txt') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-center gap-2">
              <input id="field-name" v-model="form.name" type="text" autofocus :class="inputClass" />
              <span class="whitespace-nowrap text-xs text-text-muted">{{ t('mail.optional') }}</span>
            </div>
          </div>
        </div>

        <div class="flex items-start gap-4">
          <label for="field-email_local" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            <em class="pr-0.5 not-italic text-danger-text">*</em>{{ t('email_txt') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-stretch">
              <input
                id="field-email_local"
                v-model="form.email_local"
                type="text"
                class="min-w-0 flex-1 border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link"
                :class="{ 'border-danger-border': errors.email?.length }"
              />
              <span class="flex items-center border-y border-border bg-bg px-2 text-sm">@</span>
              <select
                id="field-email_domain"
                v-model="form.email_domain"
                class="min-w-[170px] border border-border bg-surface px-2 py-1.5 text-sm outline-none focus:border-link"
                :class="{ 'border-danger-border': errors.email?.length }"
              >
                <option v-for="d in domains" :key="d" :value="d">{{ d }}</option>
              </select>
            </div>
            <p v-if="errors.email?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.email.join(', ') }}
            </p>
          </div>
        </div>

        <div class="flex items-start gap-4">
          <label for="field-password" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('password_txt') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-stretch gap-0">
              <input
                id="field-password"
                v-model="form.password"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="new-password"
                class="min-w-0 flex-1 border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link"
                :class="{ 'border-danger-border': errors.password?.length }"
              />
              <button
                type="button"
                data-test="generate-password"
                class="btn btn-default whitespace-nowrap border-l-0 px-3 text-xs"
                @click="generatePassword"
              >
                {{ t('mail.generate_password') }}
              </button>
            </div>
            <p v-if="errors.password?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.password.join(', ') }}
            </p>
          </div>
        </div>

        <div class="flex items-start gap-4">
          <span class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('mail.password_strength') }}
          </span>
          <div class="flex-1 pt-1.5" data-test="password-strength">
            <div class="flex h-2 max-w-md gap-1">
              <div
                v-for="i in 5"
                :key="i"
                class="flex-1 border border-border"
                :class="strength >= 0 && i - 1 <= strength ? strengthColors[strength] : 'bg-bg'"
              />
            </div>
            <span class="text-xs text-text-muted">
              {{ strength >= 0 ? t(strengthKeys[strength]) : ' ' }}
            </span>
          </div>
        </div>

        <div class="flex items-start gap-4">
          <label for="field-repeat_password" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('mail.repeat_password') }}
          </label>
          <div class="flex-1">
            <input
              id="field-repeat_password"
              v-model="form.repeat_password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="new-password"
              :class="inputClass"
              class="max-w-md"
            />
            <p v-if="passwordsMismatch" class="mt-1 text-xs text-danger-text" data-test="password-mismatch">
              {{ t('mail.password_mismatch') }}
            </p>
            <p v-if="errors.repeat_password?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.repeat_password.join(', ') }}
            </p>
          </div>
        </div>

        <div class="flex items-start gap-4">
          <label for="field-quota" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('mail.quota_mb') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-stretch">
              <input
                id="field-quota"
                v-model="form.quota"
                type="text"
                inputmode="numeric"
                class="min-w-0 flex-1 border border-border bg-surface px-3 py-1.5 text-sm outline-none focus:border-link"
                :class="{ 'border-danger-border': errors.quota?.length }"
              />
              <span class="flex items-center border border-l-0 border-border bg-bg px-2 text-sm">MB</span>
            </div>
            <p class="mt-1 text-xs text-text-muted">{{ t('mail.quota_unlimited_hint') }}</p>
            <p v-if="errors.quota?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.quota.join(', ') }}
            </p>
          </div>
        </div>

        <div class="flex items-start gap-4">
          <label for="field-cc" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('cc_txt') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-center gap-2">
              <input id="field-cc" v-model="form.cc" type="text" :class="inputClass" />
              <span class="whitespace-nowrap text-xs text-text-muted">{{ t('mail.optional') }}</span>
            </div>
            <p class="mt-1 text-xs text-text-muted">{{ t('mail.cc_note') }}</p>
            <p v-if="errors.cc?.length" class="mt-1 text-xs text-danger-text">{{ errors.cc.join(', ') }}</p>
          </div>
        </div>

        <!-- PHP mailbox tab: forward_in_lda sits between CC and BCC -->
        <div class="flex items-start gap-4">
          <label for="field-forward_in_lda" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('forward_in_lda_txt') }}
          </label>
          <div class="flex-1"><input id="field-forward_in_lda" v-model="form.forward_in_lda" type="checkbox" class="mt-2" /></div>
        </div>

        <div class="flex items-start gap-4">
          <label for="field-sender_cc" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('sender_cc_txt') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-center gap-2">
              <input id="field-sender_cc" v-model="form.sender_cc" type="text" :class="inputClass" />
              <span class="whitespace-nowrap text-xs text-text-muted">{{ t('mail.optional') }}</span>
            </div>
            <p class="mt-1 text-xs text-text-muted">{{ t('mail.sender_cc_note') }}</p>
            <p v-if="errors.sender_cc?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.sender_cc.join(', ') }}
            </p>
          </div>
        </div>

        <div v-if="isAdmin" class="flex items-start gap-4">
          <label for="field-imap_prefix" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('mail.imap_prefix') }}
          </label>
          <div class="flex-1">
            <div class="flex max-w-md items-center gap-2">
              <input id="field-imap_prefix" v-model="form.imap_prefix" type="text" :class="inputClass" />
              <span class="whitespace-nowrap text-xs text-text-muted">{{ t('mail.optional') }}</span>
            </div>
            <p class="mt-1 text-xs text-text-muted">{{ t('mail.imap_prefix_hint') }}</p>
          </div>
        </div>

        <div v-if="policiesAvailable" class="flex items-start gap-4">
          <label for="field-policy" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('policy_txt') }}
          </label>
          <div class="flex-1">
            <select id="field-policy" v-model="form.policy" :class="inputClass" class="max-w-md">
              <option value="0">{{ t('mail.inherit_policy') }}</option>
              <option v-for="p in policies" :key="p.value" :value="p.value">{{ p.label }}</option>
            </select>
          </div>
        </div>

        <div
          v-for="cb in ([
            ['postfix', 'postfix_txt'],
            ['disablesmtp', 'disablesmtp_txt'],
            ['disabledeliver', 'disabledeliver_txt'],
            ['greylisting', 'greylisting_txt'],
            ['disableimap', 'disableimap_txt'],
            ['disablepop3', 'disablepop3_txt'],
          ] as const)"
          :key="cb[0]"
          class="flex items-start gap-4"
        >
          <label :for="`field-${cb[0]}`" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t(cb[1]) }}
          </label>
          <div class="flex-1">
            <input :id="`field-${cb[0]}`" v-model="form[cb[0]]" type="checkbox" class="mt-2" />
          </div>
        </div>
      </div>

      <!-- Autoresponder tab -->
      <div v-show="activeTab === 'autoresponder'" id="tabpanel-autoresponder" role="tabpanel" class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="field-autoresponder" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('autoresponder_txt') }}
          </label>
          <div class="flex-1"><input id="field-autoresponder" v-model="form.autoresponder" type="checkbox" class="mt-2" /></div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-autoresponder_subject" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('autoresponder_subject_txt') }}
          </label>
          <div class="flex-1">
            <input id="field-autoresponder_subject" v-model="form.autoresponder_subject" type="text" :class="inputClass" class="max-w-md" />
          </div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-autoresponder_text" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('autoresponder_text_txt') }}
          </label>
          <div class="flex-1">
            <textarea id="field-autoresponder_text" v-model="form.autoresponder_text" rows="6" :class="inputClass" class="max-w-md" />
          </div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-autoresponder_start_date" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('autoresponder_start_date_txt') }}
          </label>
          <div class="flex-1">
            <input id="field-autoresponder_start_date" v-model="form.autoresponder_start_date" type="text" :class="inputClass" class="max-w-md" />
            <p v-if="errors.autoresponder_start_date?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.autoresponder_start_date.join(', ') }}
            </p>
          </div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-autoresponder_end_date" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('autoresponder_end_date_txt') }}
          </label>
          <div class="flex-1">
            <input id="field-autoresponder_end_date" v-model="form.autoresponder_end_date" type="text" :class="inputClass" class="max-w-md" />
            <p v-if="errors.autoresponder_end_date?.length" class="mt-1 text-xs text-danger-text">
              {{ errors.autoresponder_end_date.join(', ') }}
            </p>
          </div>
        </div>
      </div>

      <!-- Mail Filter tab (legacy filter_records) -->
      <div v-show="activeTab === 'filter_records'" id="tabpanel-filter_records" role="tabpanel" class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="field-move_junk" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('move_junk_txt') }}
          </label>
          <div class="flex-1">
            <select id="field-move_junk" v-model="form.move_junk" :class="inputClass" class="max-w-md">
              <option value="y">{{ t('move_junk_before_txt') }}</option>
              <option value="a">{{ t('move_junk_after_txt') }}</option>
              <option value="n">{{ t('no_txt') }}</option>
            </select>
          </div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-purge_trash_days" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('purge_trash_days_txt') }}
          </label>
          <div class="flex-1">
            <input id="field-purge_trash_days" v-model="form.purge_trash_days" type="text" inputmode="numeric" :class="inputClass" class="max-w-md" />
          </div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-purge_junk_days" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('purge_junk_days_txt') }}
          </label>
          <div class="flex-1">
            <input id="field-purge_junk_days" v-model="form.purge_junk_days" type="text" inputmode="numeric" :class="inputClass" class="max-w-md" />
          </div>
        </div>
        <p class="pl-52 text-xs text-muted">{{ t('mail.filter_rules_list_hint') }}</p>
      </div>

      <!-- Custom Rules tab (legacy mailfilter) -->
      <div v-show="activeTab === 'mailfilter'" id="tabpanel-mailfilter" role="tabpanel" class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="field-custom_mailfilter" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('custom_mailfilter_txt') }}
          </label>
          <div class="flex-1">
            <textarea id="field-custom_mailfilter" v-model="form.custom_mailfilter" rows="12" :class="inputClass" class="max-w-xl font-mono text-xs" />
            <p class="mt-1 text-xs text-muted">{{ t('mail.custom_rules_hint') }}</p>
          </div>
        </div>
      </div>

      <!-- Backup tab -->
      <div v-show="activeTab === 'backup'" id="tabpanel-backup" role="tabpanel" class="space-y-4 px-3 py-6">
        <div class="flex items-start gap-4">
          <label for="field-backup_interval" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('backup_interval_txt') }}
          </label>
          <div class="flex-1">
            <select id="field-backup_interval" v-model="form.backup_interval" :class="inputClass" class="max-w-md">
              <option value="none">{{ t('no_backup_txt') }}</option>
              <option value="daily">{{ t('daily_backup_txt') }}</option>
              <option value="weekly">{{ t('weekly_backup_txt') }}</option>
              <option value="monthly">{{ t('monthly_backup_txt') }}</option>
            </select>
          </div>
        </div>
        <div class="flex items-start gap-4">
          <label for="field-backup_copies" class="w-48 shrink-0 pt-1.5 text-right text-sm font-semibold after:content-[':']">
            {{ t('backup_copies_txt') }}
          </label>
          <div class="flex-1">
            <select id="field-backup_copies" v-model="form.backup_copies" :class="inputClass" class="max-w-md">
              <option v-for="n in ['1','2','3','4','5','6','7','8','9','10','15','20','30']" :key="n" :value="n">{{ n }}</option>
            </select>
          </div>
        </div>
      </div>

      <!-- Save / Cancel -->
      <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
        <button
          type="button"
          data-test="form-cancel"
          class="btn btn-default px-8"
          :disabled="saving"
          @click="router.push('/mail/mailboxes')"
        >
          {{ t('form.cancel') }}
        </button>
        <button type="submit" data-test="form-save" class="btn btn-success px-8" :disabled="saving">
          {{ t('form.save') }}
        </button>
      </div>
    </form>
  </div>
</template>

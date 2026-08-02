<script setup lang="ts">
// Login page: centered card with logo placeholder, username/password and
// "stay logged in", following the ISPConfig3 login layout.
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from '../i18n'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const stayLoggedIn = ref(false)
const submitting = ref(false)

async function submit() {
  submitting.value = true
  const ok = await auth.login(username.value, password.value, stayLoggedIn.value)
  submitting.value = false
  if (ok) router.push({ name: 'dashboard' })
}
</script>

<template>
  <div class="flex min-h-full items-center justify-center bg-bg px-4">
    <div class="w-full max-w-sm border border-border bg-surface shadow-sm">
      <div class="border-b border-border bg-info px-4 py-3">
        <span class="text-base font-bold text-brand">{{ t('app.title') }}</span>
      </div>
      <form class="space-y-4 p-6" @submit.prevent="submit">
        <div
          v-if="auth.error"
          class="border border-danger-border bg-danger px-3 py-2 text-sm text-danger-text"
        >
          {{ t(auth.error) }}
        </div>
        <div>
          <label class="mb-1 block text-sm font-semibold" for="login-username">
            {{ t('login.username') }}
          </label>
          <input
            id="login-username"
            v-model="username"
            type="text"
            required
            autofocus
            autocomplete="username"
            class="w-full border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-link"
          />
        </div>
        <div>
          <label class="mb-1 block text-sm font-semibold" for="login-password">
            {{ t('login.password') }}
          </label>
          <input
            id="login-password"
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            class="w-full border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-link"
          />
        </div>
        <div class="flex items-center justify-between">
          <label class="flex items-center gap-2 text-sm">
            <input v-model="stayLoggedIn" type="checkbox" />
            {{ t('login.stay_logged_in') }}
          </label>
          <button
            type="submit"
            :disabled="submitting"
            class="btn btn-success px-6 py-2"
          >
            {{ t('login.submit') }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

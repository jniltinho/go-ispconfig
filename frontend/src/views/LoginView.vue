<script setup lang="ts">
// Login page: centered card with logo placeholder, username/password and
// "stay logged in", following the ISPConfig3 login layout.
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '../components/UiAlert.vue'
import { useI18n } from '../i18n'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const stayLoggedIn = ref(false)
const submitting = ref(false)
const showLostHint = ref(false)

async function submit() {
  submitting.value = true
  const ok = await auth.login(username.value, password.value, stayLoggedIn.value)
  submitting.value = false
  if (ok) router.push({ name: 'dashboard' })
}
</script>

<template>
  <div class="flex min-h-full items-start justify-center bg-bg px-4 pt-[15vh]">
    <div class="w-full max-w-sm border border-border bg-surface shadow-sm">
      <!-- Legacy login head: centered brand on a plain white band. -->
      <div class="flex items-center justify-center gap-3 border-b border-border bg-surface px-4 py-6">
        <img
          src="/icon-isp-panel-256.png"
          alt=""
          width="72"
          height="72"
          class="size-18 shrink-0"
        />
        <span class="text-xl font-bold text-brand">{{ t('app.title') }}</span>
      </div>
      <form class="space-y-4 p-6" @submit.prevent="submit">
        <UiAlert v-if="auth.error" variant="danger" :messages="[t(auth.error)]" />
        <div>
          <label class="sr-only" for="login-username">
            {{ t('login.username') }}
          </label>
          <input
            id="login-username"
            v-model="username"
            type="text"
            required
            autofocus
            autocomplete="username"
            :placeholder="t('login.username')"
            class="w-full border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-link"
          />
        </div>
        <div>
          <label class="sr-only" for="login-password">
            {{ t('login.password') }}
          </label>
          <input
            id="login-password"
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            :placeholder="t('login.password')"
            class="w-full border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-link"
          />
        </div>
        <!-- Legacy action row: buttons right-aligned under the fields. -->
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-2 text-sm">
            <input v-model="stayLoggedIn" type="checkbox" />
            {{ t('login.stay_logged_in') }}
          </label>
          <div class="ml-auto flex items-center gap-2">
            <button type="submit" :disabled="submitting" class="btn btn-success px-6 py-2">
              {{ t('login.submit') }}
            </button>
            <button
              type="button"
              data-test="password-lost"
              class="btn btn-default px-4 py-2"
              :aria-expanded="showLostHint"
              aria-controls="password-lost-hint"
              @click="showLostHint = !showLostHint"
            >
              {{ t('login.password_lost') }}
            </button>
          </div>
        </div>
        <UiAlert v-if="showLostHint" id="password-lost-hint" variant="info">
          {{ t('login.password_lost_hint') }}
        </UiAlert>
      </form>
    </div>
  </div>
</template>

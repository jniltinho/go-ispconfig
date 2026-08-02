<script setup lang="ts">
// Authenticated shell: topbar (logo, module tabs icon-over-title, global
// search, red logout), per-module sidebar with sections, routed content area.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Moon, Sun } from 'lucide-vue-next'
import { moduleIcons, utilityIcons } from '../icons'
import { isDark, toggleTheme } from '../theme'
import { modules } from '../modules'
import { useI18n } from '../i18n'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const search = ref('')

const activeModule = computed(
  () => modules.find((m) => route.path.startsWith(m.path)) ?? modules[0],
)

// Admin-only sidebar sections (e.g. DNS templates) are hidden from clients.
const visibleSections = computed(() =>
  activeModule.value.sections.filter((s) => !s.adminOnly || auth.typ === 'admin'),
)

async function logout() {
  await auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="flex min-h-full flex-col">
    <header class="border-b border-border bg-surface">
      <div class="flex items-center gap-6 px-5">
        <!-- Logo placeholder (white-label logo arrives with add-panel-ui-theme) -->
        <RouterLink to="/dashboard" class="py-3 text-lg font-bold text-brand no-underline">
          {{ t('app.title') }}
        </RouterLink>

        <!-- Module tabs: 32px icon over bold title (original top-nav) -->
        <nav class="flex flex-1 justify-center">
          <RouterLink
            v-for="mod in modules"
            :key="mod.id"
            :to="mod.path"
            class="flex w-24 flex-col items-center gap-1 border-b-2 border-transparent py-2 text-text no-underline transition-colors duration-150 hover:bg-bg hover:text-brand"
            :class="{ 'border-brand! bg-bg text-brand': activeModule.id === mod.id }"
          >
            <component :is="moduleIcons[mod.id]" :size="32" :stroke-width="1.5" />
            <span class="text-xs font-bold">{{ t(`module.${mod.id}`) }}</span>
          </RouterLink>
        </nav>

        <!-- Global search + logout -->
        <div class="flex items-center gap-3">
          <div class="flex items-center border border-border bg-surface focus-within:border-link">
            <input
              v-model="search"
              type="search"
              :placeholder="t('topbar.search_placeholder')"
              class="w-40 bg-surface px-2 py-1.5 text-sm text-text outline-none"
            />
            <component :is="utilityIcons.search" :size="16" class="mx-2 text-text" />
          </div>
          <button
            type="button"
            data-test="theme-toggle"
            :title="t(isDark ? 'topbar.light_mode' : 'topbar.dark_mode')"
            :aria-label="t(isDark ? 'topbar.light_mode' : 'topbar.dark_mode')"
            class="border border-border bg-surface p-2 text-text transition-colors duration-150 hover:bg-info"
            @click="toggleTheme"
          >
            <component :is="isDark ? Sun : Moon" :size="16" />
          </button>
          <button
            type="button"
            class="bg-brand px-4 py-2 text-xs font-bold text-white hover:opacity-90"
            @click="logout"
          >
            {{ t('topbar.logout', { username: auth.username }) }}
          </button>
        </div>
      </div>
    </header>

    <div class="flex flex-1">
      <!-- Per-module sidebar (215px like the original, fluid content) -->
      <aside class="w-[215px] shrink-0 border-r border-border bg-surface">
        <div class="border-b border-border bg-info px-4 py-2.5 text-sm font-bold text-info-text">
          {{ t(`module.${activeModule.id}`) }}
        </div>
        <ul>
          <li v-for="section in visibleSections" :key="section.labelKey">
            <RouterLink
              :to="section.path"
              class="block border-l-2 border-transparent px-4 py-2.5 text-sm text-text no-underline transition-colors duration-150 hover:bg-info [&.router-link-active]:border-brand [&.router-link-active]:bg-bg [&.router-link-active]:font-semibold"
            >
              {{ t(section.labelKey) }}
            </RouterLink>
          </li>
        </ul>
      </aside>

      <!-- Fluid content area with consistent gutters (no fixed 950px) -->
      <main class="min-w-0 flex-1 p-5">
        <RouterView />
      </main>
    </div>
  </div>
</template>

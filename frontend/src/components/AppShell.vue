<script setup lang="ts">
// Authenticated shell: topbar (logo, module tabs icon-over-title, global
// search, red logout), per-module sidebar with sections, routed content area.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Menu, Moon, Sun } from 'lucide-vue-next'
import { moduleIcons, utilityIcons } from '../icons'
import { isDark, toggleTheme } from '../theme'
import { modules } from '../modules'
import { useI18n } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useUiStore } from '../stores/ui'

const { t } = useI18n()
const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const router = useRouter()
const search = ref('')

const activeModule = computed(
  () => modules.find((m) => route.path.startsWith(m.path)) ?? modules[0],
)

// Module visibility follows the session's sys_user.modules CSV (spec
// client-ui): admins see everything, dashboard is always shown.
const visibleModules = computed(() =>
  modules.filter(
    (m) => m.id === 'dashboard' || auth.typ === 'admin' || auth.modules.includes(m.id),
  ),
)

// Admin-only sidebar sections (e.g. DNS templates) are hidden from clients.
const visibleSections = computed(() =>
  activeModule.value.sections.filter((s) => !s.adminOnly || auth.typ === 'admin'),
)

// Navigating closes the drawer (mobile: tap a section, see the content).
watch(
  () => route.path,
  () => ui.closeSidebar(),
)

// Drawer a11y: Escape closes it and the page behind stops scrolling.
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') ui.closeSidebar()
}
watch(
  () => ui.sidebarOpen,
  (open) => {
    document.body.classList.toggle('overflow-hidden', open)
    if (open) window.addEventListener('keydown', onKeydown)
    else window.removeEventListener('keydown', onKeydown)
  },
)

async function logout() {
  await auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="flex min-h-full flex-col">
    <!-- Indeterminate top progress bar (nprogress-style). pointer-events
         none so it never steals clicks from the topbar. -->
    <div
      v-show="ui.routeLoading"
      class="route-progress"
      role="progressbar"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-label="t('nav.loading')"
    />
    <header class="border-b border-border bg-surface">
      <div class="flex items-center gap-6 px-5">
        <button
          type="button"
          data-test="sidebar-toggle"
          :aria-label="t('sidebar.toggle')"
          :aria-expanded="ui.sidebarOpen"
          aria-controls="module-sidebar"
          class="border border-border bg-surface p-2 text-text hover:bg-info lg:hidden"
          @click="ui.toggleSidebar"
        >
          <Menu :size="18" />
        </button>
        <!-- Logo placeholder (white-label logo arrives with add-panel-ui-theme) -->
        <RouterLink to="/dashboard" class="py-3 text-lg font-bold text-brand no-underline">
          {{ t('app.title') }}
        </RouterLink>

        <!-- Module tabs: 32px icon over bold title (original top-nav) -->
        <nav class="flex min-w-0 flex-1 justify-center overflow-x-auto">
          <RouterLink
            v-for="mod in visibleModules"
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
          <div class="flex items-center border border-border bg-surface focus-within:border-link max-md:hidden">
            <input
              v-model="search"
              type="search"
              :aria-label="t('topbar.search_placeholder')"
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
            class="btn btn-danger px-4 py-2"
            @click="logout"
          >
            {{ t('topbar.logout', { username: auth.username }) }}
          </button>
        </div>
      </div>
    </header>

    <div class="flex flex-1">
      <!-- Backdrop for the off-canvas drawer -->
      <div
        v-if="ui.sidebarOpen"
        data-test="sidebar-backdrop"
        class="fixed inset-0 z-30 bg-black/40 lg:hidden"
        @click="ui.closeSidebar"
      />
      <!-- Per-module sidebar: static >= lg, off-canvas drawer below -->
      <aside
        id="module-sidebar"
        data-test="sidebar"
        class="w-[215px] shrink-0 border-r border-border bg-surface max-lg:fixed max-lg:inset-y-0 max-lg:left-0 max-lg:z-40 max-lg:transition-transform max-lg:duration-150"
        :class="ui.sidebarOpen ? '' : 'max-lg:invisible max-lg:-translate-x-full'"
      >
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

      <!-- Fluid content area with consistent gutters (no fixed 950px).
           Soft route loader overlays only <main> so topbar/sidebar stay
           clickable; aria-busy announces navigation to assistive tech. -->
      <main class="relative min-w-0 flex-1 p-5" :aria-busy="ui.routeLoading">
        <div
          v-if="ui.routeLoading"
          class="route-overlay"
          aria-live="polite"
          :aria-label="t('nav.loading')"
        >
          <div class="route-spinner" aria-hidden="true" />
        </div>
        <RouterView v-slot="{ Component }">
          <Transition name="route-fade" mode="out-in">
            <component :is="Component" :key="route.path" />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>
</template>

// UI chrome state: off-canvas sidebar drawer (add-panel-ui-theme D6) and
// soft route-transition loading indicator when switching module tabs/sections.
import { defineStore } from 'pinia'

/** Delay before the loader becomes visible — skips flash on instant nav. */
const SHOW_DELAY_MS = 100
/** Once shown, keep the loader up at least this long to avoid a blink. */
const MIN_VISIBLE_MS = 200

// Module-level timers (not reactive state) so rapid start/stop pairs
// coalesce without thrashing the progress bar / overlay.
let showTimer: ReturnType<typeof setTimeout> | null = null
let hideTimer: ReturnType<typeof setTimeout> | null = null
let shownAt = 0
/** True while a navigation is still in flight (may not yet be visible). */
let pending = false

function clearShowTimer() {
  if (showTimer !== null) {
    clearTimeout(showTimer)
    showTimer = null
  }
}

function clearHideTimer() {
  if (hideTimer !== null) {
    clearTimeout(hideTimer)
    hideTimer = null
  }
}

export const useUiStore = defineStore('ui', {
  state: () => ({
    /** sidebarOpen controls the off-canvas drawer below lg. */
    sidebarOpen: false,
    /**
     * routeLoading is true while the soft navigation loader is visible.
     * Drives the top progress bar + content spinner so tab changes feel
     * less abrupt when the next view mounts empty and then fetches.
     */
    routeLoading: false,
  }),
  actions: {
    /** toggleSidebar flips the drawer. */
    toggleSidebar() {
      this.sidebarOpen = !this.sidebarOpen
    },
    /** closeSidebar hides the drawer (backdrop click, navigation). */
    closeSidebar() {
      this.sidebarOpen = false
    },
    /**
     * startRouteLoading schedules the soft navigation loader.
     * A short delay avoids flashing on instant navigations; if stop
     * arrives before the delay elapses, nothing is shown.
     */
    startRouteLoading() {
      pending = true
      clearHideTimer()
      // Already visible or already waiting to show — leave as-is.
      if (this.routeLoading || showTimer !== null) return

      showTimer = setTimeout(() => {
        showTimer = null
        if (!pending) return
        this.routeLoading = true
        shownAt = Date.now()
      }, SHOW_DELAY_MS)
    },
    /**
     * stopRouteLoading hides the navigation loader.
     * If the loader never became visible, the pending show is cancelled.
     * If it is visible, enforce MIN_VISIBLE_MS so it does not blink.
     */
    stopRouteLoading() {
      pending = false
      clearShowTimer()

      if (!this.routeLoading) return

      const elapsed = Date.now() - shownAt
      const remaining = Math.max(0, MIN_VISIBLE_MS - elapsed)

      clearHideTimer()
      hideTimer = setTimeout(() => {
        hideTimer = null
        // A new navigation may have started during the min-visible wait.
        if (!pending) this.routeLoading = false
      }, remaining)
    },
  },
})

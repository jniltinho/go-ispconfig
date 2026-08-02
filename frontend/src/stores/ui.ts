// UI chrome state: the off-canvas sidebar drawer used below the lg
// breakpoint (add-panel-ui-theme D6).
import { defineStore } from 'pinia'

export const useUiStore = defineStore('ui', {
  state: () => ({
    /** sidebarOpen controls the off-canvas drawer below lg. */
    sidebarOpen: false,
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
  },
})

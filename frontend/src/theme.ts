// Dark-mode state (add-panel-ui-theme D2): the `.dark` class on <html>
// re-assigns the semantic tokens. The preference persists in
// localStorage('theme'); with no stored preference the OS
// prefers-color-scheme wins. index.html applies the class inline before
// the bundle loads so a reload never flashes the wrong scheme.
import { ref } from 'vue'

const STORAGE_KEY = 'theme'

/** prefersDark reports the current effective preference.
 * Mirrored by the inline no-flash script in index.html — keep in sync. */
function prefersDark(): boolean {
  let stored: string | null = null
  try {
    stored = localStorage.getItem(STORAGE_KEY)
  } catch {
    // Restricted storage: fall through to the OS preference.
  }
  if (stored === 'dark') return true
  if (stored === 'light') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

/** isDark is the reactive scheme state used by the topbar toggle. */
export const isDark = ref(prefersDark())

/** applyTheme stamps the class; exported for init on app boot. */
export function applyTheme(): void {
  document.documentElement.classList.toggle('dark', isDark.value)
}

/** toggleTheme flips the scheme and persists the explicit choice. */
export function toggleTheme(): void {
  isDark.value = !isDark.value
  try {
    localStorage.setItem(STORAGE_KEY, isDark.value ? 'dark' : 'light')
  } catch {
    // Restricted storage: the choice still applies for this session.
  }
  applyTheme()
}

// Without a stored choice, follow live OS scheme changes.
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (ev) => {
  let stored: string | null = null
  try {
    stored = localStorage.getItem(STORAGE_KEY)
  } catch {
    // ignore
  }
  if (stored !== 'dark' && stored !== 'light') {
    isDark.value = ev.matches
    applyTheme()
  }
})

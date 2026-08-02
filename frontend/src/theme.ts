// Dark-mode state (add-panel-ui-theme D2): the `.dark` class on <html>
// re-assigns the semantic tokens. The preference persists in
// localStorage('theme'); with no stored preference the OS
// prefers-color-scheme wins. index.html applies the class inline before
// the bundle loads so a reload never flashes the wrong scheme.
import { ref } from 'vue'

const STORAGE_KEY = 'theme'

/** prefersDark reports the current effective preference. */
function prefersDark(): boolean {
  const stored = localStorage.getItem(STORAGE_KEY)
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
  localStorage.setItem(STORAGE_KEY, isDark.value ? 'dark' : 'light')
  applyTheme()
}

// Light i18n layer replacing ISPConfig's .lng PHP arrays.
// Flat JSON key/value locale files; `en` is the shipped default and the
// fallback. Adding a language = adding a JSON file to src/locales/ and
// registering it in `locales` below.
import en from './locales/en.json'

type Messages = Record<string, string>

const locales: Record<string, Messages> = { en }

let current = 'en'

/** setLocale switches the active locale (falls back to en if unknown). */
export function setLocale(locale: string): void {
  current = locale in locales ? locale : 'en'
}

function lookup(key: string): string | undefined {
  const active = locales[current]
  if (active && key in active) return active[key]
  // Fallback to English, warning in dev so missing keys get noticed.
  if (import.meta.env.DEV && current !== 'en') {
    console.warn(`[i18n] missing key "${key}" in locale "${current}", falling back to en`)
  }
  return (en as Messages)[key]
}

/** t translates a key, interpolating {param} placeholders. */
export function t(key: string, params?: Record<string, string | number>): string {
  let msg: string
  const found = lookup(key)
  if (found === undefined) {
    if (import.meta.env.DEV) console.warn(`[i18n] missing key "${key}" in every locale`)
    msg = key
  } else {
    msg = found
  }
  if (params) {
    for (const [name, value] of Object.entries(params)) {
      msg = msg.replaceAll(`{${name}}`, String(value))
    }
  }
  return msg
}

/** useI18n exposes the translate function to components. */
export function useI18n() {
  return { t, setLocale }
}

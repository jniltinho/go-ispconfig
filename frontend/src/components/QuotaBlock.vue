<script setup lang="ts">
// Quota dashlet (port of dashlets/templates/quota.htm, mailquota.htm and
// databasequota.htm): one row per site/mailbox/database with a used-vs-limit
// bar coloured like the legacy theme (>=80% orange, >=100% red).
import { useI18n } from '../i18n'

export interface QuotaRow {
  /** name is the site domain, mailbox address or database name. */
  name: string
  /** used is the current usage in bytes. */
  used: number
  /** total is the limit in bytes; 0 or less means unlimited. */
  total: number
}

defineProps<{ title: string; rows: QuotaRow[] }>()

const { t } = useI18n()

/** human renders bytes with the same units the legacy dashlets print. */
function human(bytes: number): string {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  const v = bytes / 1024 ** i
  return `${v >= 100 || i === 0 ? Math.round(v) : Math.round(v * 10) / 10} ${units[i]}`
}

function percent(r: QuotaRow): number {
  if (r.total <= 0 || r.used <= 0) return 0
  return Math.min(100, Math.round((r.used * 100) / r.total))
}

/** barColour mirrors the legacy display_colour thresholds. */
function barColour(r: QuotaRow): string {
  const ratio = r.total > 0 ? r.used / r.total : 0
  if (ratio >= 1) return '#cc0000'
  if (ratio >= 0.8) return '#fd934f'
  return 'rgb(75, 192, 192)'
}
</script>

<template>
  <section class="border border-border bg-dashlet p-4" :data-test="`dashlet-quota-${title}`">
    <p class="mb-2 text-xs font-bold uppercase text-text-muted">{{ title }}</p>
    <table class="w-full text-xs">
      <tbody>
        <tr v-for="r in rows" :key="r.name" class="align-middle">
          <td class="py-1 pr-2">{{ r.name }}</td>
          <td class="w-1/3 py-1">
            <div class="h-2 w-full bg-border" role="presentation">
              <div
                class="h-2"
                :style="{ width: `${percent(r)}%`, background: barColour(r) }"
              />
            </div>
          </td>
          <td class="py-1 pl-2 text-right whitespace-nowrap text-text-muted">
            {{ human(r.used) }} /
            {{ r.total > 0 ? human(r.total) : t('dashboard.quota.unlimited') }}
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

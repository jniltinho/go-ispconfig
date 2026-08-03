<script setup lang="ts">
// Account limits dashlet (port of dashlets/limits.php + templates/limits.htm):
// one row per limit_* column with usage, limit and a progress bar. -1 means
// unlimited and gets no bar, exactly like the legacy template.
import { useI18n } from '../i18n'

export interface LimitRow {
  /** field is the client column name, e.g. limit_web_domain. */
  field: string
  /** limit is the configured cap; -1 means unlimited. */
  limit: number
  /** usage is the current count, or assigned MB when quota is true. */
  usage: number
  /** quota marks rows measured in megabytes instead of records. */
  quota?: boolean
}

// unlimited: admins have no client row, so the dashlet just says so.
defineProps<{ rows: LimitRow[]; unlimited?: boolean }>()

const { t } = useI18n()

function percent(r: LimitRow): number {
  if (r.limit <= 0) return 0
  return Math.min(100, Math.round((r.usage * 100) / r.limit))
}

/** barColour mirrors the legacy display_colour thresholds. */
function barColour(r: LimitRow): string {
  const ratio = r.limit > 0 ? r.usage / r.limit : 0
  if (ratio >= 1) return '#cc0000'
  if (ratio >= 0.8) return '#fd934f'
  return 'rgb(75, 192, 192)'
}

function value(r: LimitRow): string {
  if (r.limit < 0) return t('limits.unlimited')
  return r.quota ? `${r.limit} MB` : String(r.limit)
}

function used(r: LimitRow): string {
  return r.quota ? `${r.usage} MB` : String(r.usage)
}
</script>

<template>
  <section class="border border-border bg-dashlet p-4" data-test="dashlet-limits">
    <p class="mb-2 text-xs font-bold uppercase text-text-muted">{{ t('limits.title') }}</p>
    <p v-if="unlimited" class="text-xs">{{ t('limits.unlimited') }}</p>
    <table v-else class="w-full text-xs">
      <tbody>
        <tr v-for="r in rows" :key="r.field" class="align-middle">
          <td class="py-1 pr-2">{{ t(`limits.${r.field}`) }}</td>
          <td class="w-1/3 py-1">
            <div v-if="r.limit > 0" class="h-2 w-full bg-border" role="presentation">
              <div class="h-2" :style="{ width: `${percent(r)}%`, background: barColour(r) }" />
            </div>
          </td>
          <td class="py-1 pl-2 text-right whitespace-nowrap text-text-muted">
            {{ used(r) }} / {{ value(r) }}
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

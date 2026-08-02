<script setup lang="ts">
// Alerts in the original ISPConfig3 composition (add-panel-ui-theme 3.5):
// info = .alert-notification (#DFEAF6), danger = .alert-danger (#F7DFDF)
// with the leading "Error" label block and a message list. Single root so
// class/id fallthrough from callers works.
import { useI18n } from '../i18n'

defineProps<{
  variant: 'info' | 'danger'
  /** messages fills the list; the default slot wins when present. */
  messages?: string[]
}>()

const { t } = useI18n()
</script>

<template>
  <div
    :role="variant === 'danger' ? 'alert' : 'status'"
    :aria-live="variant === 'danger' ? undefined : 'polite'"
    :data-test="`alert-${variant}`"
    class="border text-sm"
    :class="
      variant === 'danger'
        ? 'flex border-danger-border bg-danger text-danger-text'
        : 'border-info-border bg-info px-3 py-2 text-info-text'
    "
  >
    <span
      v-if="variant === 'danger'"
      class="w-[60px] shrink-0 self-stretch bg-danger-border/50 px-2 py-2 font-bold"
    >
      {{ t('form.error_label') }}
    </span>
    <div :class="variant === 'danger' ? 'py-2 pl-3 pr-3' : ''">
      <slot>
        <ul v-if="messages?.length" class="list-disc pl-5">
          <li v-for="(msg, i) in messages" :key="i">{{ msg }}</li>
        </ul>
      </slot>
    </div>
  </div>
</template>

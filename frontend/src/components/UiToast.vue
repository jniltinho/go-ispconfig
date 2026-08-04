<script setup lang="ts">
// Transient status messages, mounted once in AppShell and driven by the
// dialog store. Colours come from the same alert palette UiAlert uses, so a
// toast and an inline alert are recognisably the same language.
//
// The container is aria-live="polite": screen readers announce a toast when
// they finish the current utterance, which is the right urgency for "saved"
// and for a background failure alike. It stays in the DOM even when empty so
// the live region is established before the first message arrives.
import { useDialogStore } from '../stores/dialog'
import { useI18n } from '../i18n'
import { utilityIcons } from '../icons'

const { t } = useI18n()
const dialog = useDialogStore()

const variantClass = {
  success: 'border-success bg-success text-white',
  danger: 'border-danger-border bg-danger text-danger-text',
  info: 'border-info-border bg-info text-info-text',
} as const
</script>

<template>
  <div
    class="pointer-events-none fixed bottom-4 right-4 z-50 flex flex-col gap-2"
    role="status"
    aria-live="polite"
    data-test="ui-toasts"
  >
    <TransitionGroup name="toast">
      <div
        v-for="toast in dialog.toasts"
        :key="toast.id"
        class="pointer-events-auto flex max-w-sm items-start gap-2 border px-3 py-2 text-sm shadow-lg"
        :class="variantClass[toast.variant]"
        :data-test="`toast-${toast.variant}`"
      >
        <span class="flex-1">{{ toast.message }}</span>
        <button
          type="button"
          class="shrink-0 opacity-70 hover:opacity-100"
          :aria-label="t('dialog.dismiss')"
          @click="dialog.dismissToast(toast.id)"
        >
          <component :is="utilityIcons.close" :size="14" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition:
    opacity 150ms ease,
    transform 150ms ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(0.5rem);
}

@media (prefers-reduced-motion: reduce) {
  .toast-enter-active,
  .toast-leave-active {
    transition: none;
  }
}
</style>

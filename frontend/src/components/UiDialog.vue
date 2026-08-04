<script setup lang="ts">
// Themed confirmation dialog, mounted once in AppShell and driven by the
// dialog store. Built on the native <dialog> element rather than a div with
// a hand-rolled overlay: the platform already gives focus trapping, Esc to
// close, inertness of the page behind, and the top layer that no z-index
// stacking context can cover.
//
// `m-auto` is not decoration: the browser centres a modal <dialog> with
// `margin: auto`, and Tailwind's preflight resets margins to 0, which pins
// the dialog to the top-left corner unless it is put back.
import { ref, watch } from 'vue'
import { useDialogStore } from '../stores/dialog'
import { useI18n } from '../i18n'

const { t } = useI18n()
const dialog = useDialogStore()
const el = ref<HTMLDialogElement | null>(null)

watch(
  () => dialog.request,
  (request) => {
    const node = el.value
    if (!node) return
    if (request) {
      if (!node.open) node.showModal()
    } else if (node.open) {
      node.close()
    }
  },
)

// Esc and the browser's own close path both fire `cancel`/`close`; route
// them through the store so the pending promise always resolves.
function onClose() {
  if (dialog.request) dialog.answer(false)
}
</script>

<template>
  <dialog
    ref="el"
    class="m-auto w-[min(28rem,calc(100vw-2rem))] border border-border bg-surface p-0 text-text backdrop:bg-scrim"
    data-test="ui-dialog"
    @close="onClose"
  >
    <div v-if="dialog.request" class="flex flex-col">
      <h2 class="border-b border-border bg-bg px-4 py-3 text-sm font-bold">
        {{ dialog.request.title ?? t('dialog.confirm_title') }}
      </h2>

      <p class="px-4 py-5 text-sm" data-test="ui-dialog-message">
        {{ dialog.request.message }}
      </p>

      <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-3">
        <button
          type="button"
          class="btn btn-default px-6"
          data-test="ui-dialog-cancel"
          @click="dialog.answer(false)"
        >
          {{ dialog.request.cancelLabel ?? t('dialog.cancel') }}
        </button>
        <button
          type="button"
          class="btn px-6"
          :class="dialog.request.variant === 'default' ? 'btn-success' : 'btn-danger'"
          data-test="ui-dialog-confirm"
          autofocus
          @click="dialog.answer(true)"
        >
          {{ dialog.request.confirmLabel ?? t('dialog.confirm') }}
        </button>
      </div>
    </div>
  </dialog>
</template>

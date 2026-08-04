// Themed replacements for the browser's window.confirm and for the silent
// success/failure of background actions.
//
// Both are stores rather than per-view components so a call site keeps the
// shape it already had — `if (!(await confirm(...))) return` reads like the
// native call it replaces — and so exactly one dialog and one toast stack
// exist in the app, mounted once in AppShell.
import { defineStore } from 'pinia'

/** ConfirmVariant picks the accent of the confirm button. */
export type ConfirmVariant = 'danger' | 'default'

/** ConfirmRequest is what the dialog renders. */
export interface ConfirmRequest {
  /** message is the question, already translated. */
  message: string
  /** title defaults to a generic "Confirm" when omitted. */
  title?: string
  /** confirmLabel defaults to "OK". */
  confirmLabel?: string
  /** cancelLabel defaults to "Cancel". */
  cancelLabel?: string
  /**
   * variant danger (the default for delete flows) renders the confirm
   * button in the brand red, so an irreversible action does not look like
   * an ordinary one.
   */
  variant?: ConfirmVariant
}

/** ToastVariant maps onto the alert palette already in the theme. */
export type ToastVariant = 'success' | 'danger' | 'info'

/** Toast is one transient message in the stack. */
export interface Toast {
  id: number
  message: string
  variant: ToastVariant
}

/** How long a toast stays up before removing itself. */
const TOAST_TTL_MS = 4000

// The pending resolver lives outside the reactive state: it is a function,
// never rendered, and putting it in state would make Pinia proxy it.
let resolvePending: ((ok: boolean) => void) | null = null
let nextToastId = 1

export const useDialogStore = defineStore('dialog', {
  state: () => ({
    /** request is the confirm currently on screen, null when none. */
    request: null as ConfirmRequest | null,
    /** toasts is the visible stack, newest last. */
    toasts: [] as Toast[],
  }),
  actions: {
    /**
     * confirm shows the themed dialog and resolves to the user's answer.
     * Drop-in for window.confirm, minus the browser chrome:
     *
     *   if (!(await dialog.confirm({ message: t('sites.confirm_delete') }))) return
     *
     * A second call while one is open resolves the first as cancelled, so a
     * stray double-click can never leave a promise dangling forever.
     */
    confirm(request: ConfirmRequest): Promise<boolean> {
      if (resolvePending) {
        resolvePending(false)
        resolvePending = null
      }
      this.request = request
      return new Promise<boolean>((resolve) => {
        resolvePending = resolve
      })
    },
    /** answer closes the dialog with the user's decision. */
    answer(ok: boolean) {
      this.request = null
      const resolve = resolvePending
      resolvePending = null
      resolve?.(ok)
    },
    /** toast pushes a transient message that removes itself. */
    toast(message: string, variant: ToastVariant = 'success') {
      const id = nextToastId++
      this.toasts.push({ id, message, variant })
      setTimeout(() => this.dismissToast(id), TOAST_TTL_MS)
    },
    /** dismissToast removes one message (timer or the close button). */
    dismissToast(id: number) {
      this.toasts = this.toasts.filter((t) => t.id !== id)
    },
  },
})

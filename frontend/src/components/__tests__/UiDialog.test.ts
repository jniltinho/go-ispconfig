// The themed confirm dialog and the toast stack, plus the store contract
// every call site relies on: confirm() resolves to the user's answer and
// never leaves a promise dangling.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UiDialog from '../UiDialog.vue'
import UiToast from '../UiToast.vue'
import { useDialogStore } from '../../stores/dialog'

// jsdom implements <dialog> without showModal/close in some versions; stub
// them so the component's open/close bookkeeping is exercised either way.
beforeEach(() => {
  setActivePinia(createPinia())
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.open = true
  })
  HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
    this.open = false
    this.dispatchEvent(new Event('close'))
  })
})

describe('UiDialog', () => {
  it('renders the message and resolves true on confirm', async () => {
    const wrapper = mount(UiDialog)
    const dialog = useDialogStore()

    const answer = dialog.confirm({ message: 'Delete this website?' })
    await flushPromises()

    expect(wrapper.find('[data-test="ui-dialog-message"]').text()).toBe('Delete this website?')
    await wrapper.find('[data-test="ui-dialog-confirm"]').trigger('click')
    await expect(answer).resolves.toBe(true)
  })

  it('resolves false on cancel and closes', async () => {
    const wrapper = mount(UiDialog)
    const dialog = useDialogStore()

    const answer = dialog.confirm({ message: 'Delete?' })
    await flushPromises()
    await wrapper.find('[data-test="ui-dialog-cancel"]').trigger('click')

    await expect(answer).resolves.toBe(false)
    expect(dialog.request).toBeNull()
  })

  // Esc closes a native <dialog> without any click, so the close event must
  // resolve the promise — otherwise the caller waits forever.
  it('resolves false when the dialog is closed by the platform', async () => {
    const wrapper = mount(UiDialog)
    const dialog = useDialogStore()

    const answer = dialog.confirm({ message: 'Delete?' })
    await flushPromises()
    await wrapper.find('dialog').trigger('close')

    await expect(answer).resolves.toBe(false)
  })

  // A stray double-click must not strand the first promise.
  it('resolves a superseded request as cancelled', async () => {
    mount(UiDialog)
    const dialog = useDialogStore()

    const first = dialog.confirm({ message: 'First' })
    const second = dialog.confirm({ message: 'Second' })
    await flushPromises()

    await expect(first).resolves.toBe(false)
    dialog.answer(true)
    await expect(second).resolves.toBe(true)
  })

  it('uses the danger accent by default and the neutral one on request', async () => {
    const wrapper = mount(UiDialog)
    const dialog = useDialogStore()

    dialog.confirm({ message: 'Delete?' })
    await flushPromises()
    expect(wrapper.find('[data-test="ui-dialog-confirm"]').classes()).toContain('btn-danger')

    dialog.answer(false)
    dialog.confirm({ message: 'Proceed?', variant: 'default' })
    await flushPromises()
    expect(wrapper.find('[data-test="ui-dialog-confirm"]').classes()).toContain('btn-success')
  })
})

describe('UiToast', () => {
  it('shows a toast and removes it when dismissed', async () => {
    const wrapper = mount(UiToast)
    const dialog = useDialogStore()

    dialog.toast('Token copied to the clipboard.')
    await flushPromises()
    expect(wrapper.find('[data-test="toast-success"]').text()).toContain('Token copied')

    await wrapper.find('[data-test="toast-success"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="toast-success"]').exists()).toBe(false)
  })

  it('expires a toast on its own', async () => {
    vi.useFakeTimers()
    const wrapper = mount(UiToast)
    const dialog = useDialogStore()

    dialog.toast('Saved.', 'info')
    await flushPromises()
    expect(wrapper.find('[data-test="toast-info"]').exists()).toBe(true)

    vi.advanceTimersByTime(5000)
    await flushPromises()
    expect(wrapper.find('[data-test="toast-info"]').exists()).toBe(false)
    vi.useRealTimers()
  })

  it('stacks several toasts', async () => {
    const wrapper = mount(UiToast)
    const dialog = useDialogStore()

    dialog.toast('One')
    dialog.toast('Two', 'danger')
    await flushPromises()

    expect(wrapper.findAll('[data-test^="toast-"]')).toHaveLength(2)
  })
})

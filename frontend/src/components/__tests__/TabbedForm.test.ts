import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TabbedForm, { type FormMetadata } from '../TabbedForm.vue'

const metadata: FormMetadata = {
  tabs: [
    {
      name: 'main',
      label: 'Main',
      fields: [
        { name: 'domain', type: 'text', label: 'Domain' },
        { name: 'active', type: 'checkbox', label: 'Active', default: true },
      ],
    },
    {
      name: 'extra',
      label: 'Extra',
      fields: [{ name: 'notes', type: 'textarea', label: 'Notes' }],
    },
  ],
}

describe('TabbedForm', () => {
  it('renders tabs and fields from metadata with defaults', () => {
    const wrapper = mount(TabbedForm, { props: { metadata } })
    expect(wrapper.text()).toContain('Main')
    expect(wrapper.text()).toContain('Extra')
    expect(wrapper.find('#field-domain').exists()).toBe(true)
    expect((wrapper.find('#field-active').element as HTMLInputElement).checked).toBe(true)
  })

  it('emits save with the current values', async () => {
    const wrapper = mount(TabbedForm, { props: { metadata } })
    await wrapper.find('#field-domain').setValue('example.com')
    await wrapper.find('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([
      [{ domain: 'example.com', active: true, notes: '' }],
    ])
  })

  it('emits cancel and shows field errors', async () => {
    const wrapper = mount(TabbedForm, {
      props: { metadata, errors: { domain: ['Domain is invalid.'] } },
    })
    expect(wrapper.text()).toContain('Domain is invalid.')
    const cancel = wrapper.findAll('button').find((b) => b.text() === 'Cancel')
    await cancel!.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})

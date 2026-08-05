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

  it('renders legend fields as sub-headings and errors via UiAlert', () => {
    const wrapper = mount(TabbedForm, {
      props: {
        metadata: {
          tabs: [
            {
              name: 'main',
              label: 'Main',
              fields: [
                { name: 'sec', type: 'legend', label: 'Section heading' },
                { name: 'domain', type: 'text', label: 'Domain' },
              ],
            },
          ],
        },
        errors: { domain: ['is invalid'] },
      },
    })
    expect(wrapper.text()).toContain('Section heading')
    const alert = wrapper.find('[data-test="alert-danger"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('Domain: is invalid')
    expect(alert.classes()).toContain('m-4')
  })

  it('disables readonly checkboxes and buttons while saving', () => {
    const wrapper = mount(TabbedForm, {
      props: {
        metadata: {
          tabs: [
            {
              name: 'main',
              label: 'Main',
              fields: [{ name: 'active', type: 'checkbox', label: 'Active', readonly: true }],
            },
          ],
        },
        saving: true,
      },
    })
    expect((wrapper.find('#field-active').element as HTMLInputElement).disabled).toBe(true)
    expect((wrapper.find('[data-test="form-save"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(
      (wrapper.find('[data-test="form-cancel"]').element as HTMLButtonElement).disabled,
    ).toBe(true)
  })

  it('folds a collapsible legend and the fields after it into a closed <details>', () => {
    const wrapper = mount(TabbedForm, {
      props: {
        metadata: {
          tabs: [
            {
              name: 'main',
              label: 'Main',
              fields: [
                { name: 'domain', type: 'text', label: 'Domain' },
                { name: 'dkim_settings', type: 'legend', label: 'DKIM', collapsible: true },
                { name: 'dkim_selector', type: 'text', label: 'Selector' },
              ],
            },
          ],
        } as FormMetadata,
      },
    })
    const details = wrapper.find('details')
    expect(details.exists()).toBe(true)
    expect((details.element as HTMLDetailsElement).open).toBe(false)
    expect(details.find('summary').text()).toBe('DKIM')
    // Only the fields after the legend are folded; the rest stays visible.
    expect(details.find('#field-dkim_selector').exists()).toBe(true)
    expect(details.find('#field-domain').exists()).toBe(false)
    expect(wrapper.find('#field-domain').exists()).toBe(true)
  })

  it('renders the extra slot above the action bar', () => {
    const wrapper = mount(TabbedForm, {
      props: { metadata },
      slots: { extra: '<p data-test="dkim-panel">panel</p>' },
    })
    const html = wrapper.html()
    expect(html.indexOf('data-test="dkim-panel"')).toBeLessThan(
      html.indexOf('data-test="form-cancel"'),
    )
  })

  it('coerces when an option genuinely disappears between metadata rebuilds', async () => {
    const apacheOpts = [
      { value: 'no', label: 'Disabled' },
      { value: 'fast-cgi', label: 'Fast-CGI' },
    ]
    const nginxOpts = [
      { value: 'no', label: 'Disabled' },
      { value: 'php-fpm', label: 'PHP-FPM' },
    ]
    const wrapper = mount(TabbedForm, {
      props: {
        metadata: {
          tabs: [
            {
              name: 'main',
              label: 'Main',
              fields: [{ name: 'php', type: 'select', label: 'PHP', options: apacheOpts, default: 'no' }],
            },
          ],
        },
        modelValue: { php: 'fast-cgi' },
      },
    })
    expect((wrapper.find('#field-php').element as HTMLSelectElement).value).toBe('fast-cgi')
    await wrapper.setProps({
      metadata: {
        tabs: [
          {
            name: 'main',
            label: 'Main',
            fields: [{ name: 'php', type: 'select', label: 'PHP', options: nginxOpts, default: 'no' }],
          },
        ],
      },
    })
    expect((wrapper.find('#field-php').element as HTMLSelectElement).value).toBe('no')
  })
})
